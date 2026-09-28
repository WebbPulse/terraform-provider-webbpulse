package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeRegistryAPI serves the registry module and provider routes from memory
// with the API's naming, conflict and installation rules, so real Terraform
// plans can drive both resources without credentials.
type fakeRegistryAPI struct {
	mu        sync.Mutex
	modules   map[string]map[string]any
	providers map[string]map[string]any
	resyncs   []string
	deletes   []string
	creates   []map[string]any
}

func newFakeRegistryAPI() *fakeRegistryAPI {
	return &fakeRegistryAPI{modules: map[string]map[string]any{}, providers: map[string]map[string]any{}}
}

func (f *fakeRegistryAPI) fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"detail":{"message":"refused","error_code":%q}}`, code)
}

func (f *fakeRegistryAPI) canonical(repo string) (string, bool) {
	if strings.EqualFold(repo, "WebbPulse/private") {
		return "", false
	}
	owner, name, _ := strings.Cut(repo, "/")
	if strings.EqualFold(owner, "webbpulse") {
		owner = "WebbPulse"
	}
	return owner + "/" + name, true
}

func (f *fakeRegistryAPI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	route := strings.TrimPrefix(req.URL.Path, "/api/v1/registry/")
	switch {
	case req.Method == http.MethodPost && (route == "modules" || route == "providers"):
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		f.creates = append(f.creates, body)
		repo, ok := f.canonical(fmt.Sprint(body["vcs_repo"]))
		if !ok {
			f.fail(w, http.StatusUnprocessableEntity, "VCS_REPO_NOT_INSTALLED")
			return
		}
		owner, repoName, _ := strings.Cut(repo, "/")
		if route == "providers" {
			providerType, found := strings.CutPrefix(repoName, "terraform-provider-")
			if !found {
				f.fail(w, http.StatusUnprocessableEntity, "REGISTRY_INVALID_PROVIDER_NAME")
				return
			}
			key := owner + "/" + providerType
			if _, exists := f.providers[key]; exists {
				f.fail(w, http.StatusConflict, "REGISTRY_PROVIDER_EXISTS")
				return
			}
			f.providers[key] = map[string]any{
				"namespace": owner, "type": providerType, "source": key, "vcs_repo": repo,
				"created_at": "created", "versions": []any{},
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(f.providers[key])
			return
		}
		name, _ := body["name"].(string)
		provider, _ := body["provider"].(string)
		if name == "" || provider == "" {
			f.fail(w, http.StatusUnprocessableEntity, "REGISTRY_INVALID_MODULE_NAME")
			return
		}
		key := owner + "/" + name + "/" + provider
		if _, exists := f.modules[key]; exists {
			f.fail(w, http.StatusConflict, "REGISTRY_MODULE_EXISTS")
			return
		}
		f.modules[key] = map[string]any{
			"namespace": owner, "name": name, "provider": provider, "source": key, "vcs_repo": repo,
			"created_at": "created", "versions": []any{},
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.modules[key])
	case req.Method == http.MethodPost && strings.HasSuffix(route, "/resync"):
		f.resyncs = append(f.resyncs, strings.TrimSuffix(route, "/resync"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"source":"x","delivery":"sync-1"}`))
	default:
		kind, key, _ := strings.Cut(route, "/")
		store := f.modules
		if kind == "providers" {
			store = f.providers
		}
		item, ok := store[key]
		if !ok {
			f.fail(w, http.StatusNotFound, "REGISTRY_NOT_FOUND")
			return
		}
		if req.Method == http.MethodDelete {
			f.deletes = append(f.deletes, route)
			delete(store, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(item)
	}
}

func fakeRegistryConfig(url, body string) string {
	return fmt.Sprintf(`
provider "webbpulse" {
  host  = %q
  token = "synthetic-test-only"
}
%s
`, url, body)
}

// TestRegistryModuleLifecycle drives connect, the name refusal, a resync, an
// in place no-op, a replacement, import and destroy against a fake API.
func TestRegistryModuleLifecycle(t *testing.T) {
	api := newFakeRegistryAPI()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	const address = "webbpulse_registry_module.test"
	module := func(name, extra, repo string) string {
		return fakeRegistryConfig(server.URL, fmt.Sprintf(`
resource "webbpulse_registry_module" "test" {
  name            = %q
  module_provider = "aws"
%s
  vcs_repo {
    identifier = %q
  }
}
`, name, extra, repo))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			api.mu.Lock()
			defer api.mu.Unlock()
			if len(api.modules) != 0 {
				return fmt.Errorf("modules left behind: %v", api.modules)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fakeRegistryConfig(server.URL, `
resource "webbpulse_registry_module" "test" {
  vcs_repo {
    identifier = "WebbPulse/infra"
  }
}
`),
				ExpectError: regexp.MustCompile(`REGISTRY_INVALID_MODULE_NAME`),
			},
			{
				Config:      fakeRegistryConfig(server.URL, `resource "webbpulse_registry_module" "test" {}`),
				ExpectError: regexp.MustCompile(`vcs_repo`),
			},
			{
				Config:      module("network", "", "WebbPulse/private"),
				ExpectError: regexp.MustCompile(`VCS_REPO_NOT_INSTALLED`),
			},
			{
				Config: module("network", "", "webbpulse/infra"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", "WebbPulse/network/aws"),
					resource.TestCheckResourceAttr(address, "namespace", "WebbPulse"),
					resource.TestCheckResourceAttr(address, "source", "WebbPulse/network/aws"),
					resource.TestCheckResourceAttr(address, "vcs_repo.identifier", "webbpulse/infra"),
					resource.TestCheckResourceAttr(address, "import_tags", "true"),
					resource.TestCheckResourceAttr(address, "created_at", "created"),
				),
			},
			{
				Config: module("network", `  resync_triggers = { round = "1" }`, "webbpulse/infra"),
				Check: func(_ *terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					if len(api.resyncs) != 1 || api.resyncs[0] != "modules/WebbPulse/network/aws" {
						return fmt.Errorf("resyncs = %v", api.resyncs)
					}
					return nil
				},
			},
			{
				Config: module("network", `  resync_triggers = { round = "1" }
  import_tags     = false`, "webbpulse/infra"),
				Check: func(_ *terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					if len(api.resyncs) != 1 || len(api.creates) != 3 {
						return fmt.Errorf("a no-op update called the API: resyncs %v, creates %d", api.resyncs, len(api.creates))
					}
					return nil
				},
			},
			{
				Config: module("vpc", "", "WebbPulse/infra"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", "WebbPulse/vpc/aws"),
					func(_ *terraform.State) error {
						api.mu.Lock()
						defer api.mu.Unlock()
						if _, ok := api.modules["WebbPulse/network/aws"]; ok {
							return fmt.Errorf("the replaced module survived")
						}
						return nil
					},
				),
			},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateId:           "WebbPulse/vpc/aws",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"import_tags", "resync_triggers"},
			},
			{
				ResourceName:  address,
				ImportState:   true,
				ImportStateId: "WebbPulse/vpc",
				ExpectError:   regexp.MustCompile(`namespace/name/provider`),
			},
		},
	})
}

// TestRegistryProviderLifecycle drives connect, the naming and conflict
// refusals, a resync, import and destroy against a fake API.
func TestRegistryProviderLifecycle(t *testing.T) {
	api := newFakeRegistryAPI()
	api.providers["WebbPulse/taken"] = map[string]any{
		"namespace": "WebbPulse", "type": "taken", "source": "WebbPulse/taken",
		"vcs_repo": "WebbPulse/terraform-provider-taken", "versions": []any{},
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	const address = "webbpulse_registry_provider.test"
	providerConfig := func(repo, extra string) string {
		return fakeRegistryConfig(server.URL, fmt.Sprintf(`
resource "webbpulse_registry_provider" "test" {
%s
  vcs_repo {
    identifier = %q
  }
}

data "webbpulse_registry_provider" "taken" {
  namespace = "WebbPulse"
  type      = "taken"
}
`, extra, repo))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			api.mu.Lock()
			defer api.mu.Unlock()
			if _, ok := api.providers["WebbPulse/example"]; ok {
				return fmt.Errorf("the provider survived the destroy")
			}
			if _, ok := api.providers["WebbPulse/taken"]; !ok {
				return fmt.Errorf("the destroy removed a provider it did not own")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      providerConfig("WebbPulse/infra", ""),
				ExpectError: regexp.MustCompile(`REGISTRY_INVALID_PROVIDER_NAME`),
			},
			{
				Config:      providerConfig("WebbPulse/terraform-provider-taken", ""),
				ExpectError: regexp.MustCompile(`REGISTRY_PROVIDER_EXISTS`),
			},
			{
				Config: providerConfig("WebbPulse/terraform-provider-example", "  import_releases = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", "WebbPulse/example"),
					resource.TestCheckResourceAttr(address, "type", "example"),
					resource.TestCheckResourceAttr(address, "source", "WebbPulse/example"),
					resource.TestCheckResourceAttr(address, "import_releases", "false"),
					resource.TestCheckResourceAttr("data.webbpulse_registry_provider.taken", "vcs_repo", "WebbPulse/terraform-provider-taken"),
					resource.TestCheckResourceAttr("data.webbpulse_registry_provider.taken", "published_versions.#", "0"),
					func(_ *terraform.State) error {
						api.mu.Lock()
						defer api.mu.Unlock()
						last := api.creates[len(api.creates)-1]
						if last["import_releases"] != false {
							return fmt.Errorf("create body = %v", last)
						}
						return nil
					},
				),
			},
			{
				Config: providerConfig("WebbPulse/terraform-provider-example", `  import_releases = false
  resync_triggers = { at = "2026-09-27" }`),
				Check: func(_ *terraform.State) error {
					api.mu.Lock()
					defer api.mu.Unlock()
					if len(api.resyncs) != 1 || api.resyncs[0] != "providers/WebbPulse/example" {
						return fmt.Errorf("resyncs = %v", api.resyncs)
					}
					return nil
				},
			},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateId:           "WebbPulse/example",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"import_releases", "resync_triggers"},
			},
		},
	})
}
