package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// EnvAccRegistryProvider names a private provider, as namespace/type, already
// published in the target environment and connected to a repository named
// terraform-provider-<type>. The provider acceptance test imports it without
// persisting, so it is never deleted.
const EnvAccRegistryProvider = "WEBBPULSE_TF_ACC_REGISTRY_PROVIDER"

// testAccClient is an API client for checks that look past Terraform, built
// from the same environment the provider under test reads.
func testAccClient(t *testing.T) *client.Client {
	t.Helper()
	var options []client.Option
	if gate := os.Getenv(EnvAccGateHeader); gate != "" {
		options = append(options, client.WithHeader("x-origin-verify", gate))
	}
	c, err := client.New(os.Getenv(EnvHost), os.Getenv(EnvToken), options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testAccWaitForModuleVersions waits until the module has at least one version
// and none is pending, so the destroy that follows cannot race a tag import.
func testAccWaitForModuleVersions(c *client.Client, namespace, name, provider string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		deadline := time.Now().Add(4 * time.Minute)
		for {
			module, err := c.GetModule(context.Background(), namespace, name, provider)
			if err != nil {
				return err
			}
			pending := 0
			published := 0
			for _, version := range module.Versions {
				switch version.Status {
				case "pending":
					pending++
				case "published":
					published++
				}
			}
			if len(module.Versions) > 0 && pending == 0 {
				if published == 0 {
					return fmt.Errorf("no version of %s published: %+v", module.Source, module.Versions)
				}
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s still has %d pending of %d versions", module.Source, pending, len(module.Versions))
			}
			time.Sleep(5 * time.Second)
		}
	}
}

// TestAccRegistryModule connects a module to the acceptance repository,
// replaces it under a new name, resyncs its tags in place, imports it and
// destroys it, leaving nothing behind.
func TestAccRegistryModule(t *testing.T) {
	testAccPreCheck(t)
	repo := os.Getenv(EnvAccVCSRepo)
	if repo == "" {
		t.Skipf("the registry module acceptance test needs %s, a repository the environment's GitHub App is installed on", EnvAccVCSRepo)
	}
	namespace := strings.SplitN(repo, "/", 2)[0]
	first := testAccName("module")
	second := first + "-renamed"
	const address = "webbpulse_registry_module.test"
	const moduleProvider = "tfacc"
	apiClient := testAccClient(t)

	config := func(name, triggers string) string {
		return fmt.Sprintf(`
resource "webbpulse_registry_module" "test" {
  name            = %q
  module_provider = %q
  import_tags     = false
%s
  vcs_repo {
    identifier = %q
  }
}
`, name, moduleProvider, triggers, repo)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			for _, name := range []string{first, second} {
				_, err := apiClient.GetModule(context.Background(), namespace, name, moduleProvider)
				if err == nil {
					return fmt.Errorf("%s/%s/%s survived the destroy", namespace, name, moduleProvider)
				}
				if !client.IsNotFound(err) {
					return err
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "webbpulse_registry_module" "test" {
  import_tags = false

  vcs_repo {
    identifier = %q
  }
}
`, repo),
				ExpectError: regexp.MustCompile(`REGISTRY_INVALID_MODULE_NAME`),
			},
			{
				Config: config(first, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "id", namespace+"/"+first+"/"+moduleProvider),
					resource.TestCheckResourceAttr(address, "namespace", namespace),
					resource.TestCheckResourceAttr(address, "source", namespace+"/"+first+"/"+moduleProvider),
					resource.TestCheckResourceAttr(address, "vcs_repo.identifier", repo),
					resource.TestCheckResourceAttrSet(address, "created_at"),
				),
			},
			{
				Config: config(second, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "name", second),
					func(_ *terraform.State) error {
						_, err := apiClient.GetModule(context.Background(), namespace, first, moduleProvider)
						if !client.IsNotFound(err) {
							return fmt.Errorf("the replaced module is still there: %v", err)
						}
						return nil
					},
				),
			},
			{
				Config: config(second, `  resync_triggers = { round = "1" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "resync_triggers.round", "1"),
					testAccWaitForModuleVersions(apiClient, namespace, second, moduleProvider),
				),
			},
			{
				Config: config(second, `  resync_triggers = { round = "1" }`) + `
data "webbpulse_registry_module" "test" {
  namespace       = webbpulse_registry_module.test.namespace
  name            = webbpulse_registry_module.test.name
  module_provider = webbpulse_registry_module.test.module_provider
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.webbpulse_registry_module.test", "vcs_repo", repo),
					resource.TestCheckResourceAttrSet("data.webbpulse_registry_module.test", "published_versions.0"),
					resource.TestCheckResourceAttrSet("data.webbpulse_registry_module.test", "versions.0.sha"),
				),
			},
			{
				ResourceName:            address,
				ImportState:             true,
				ImportStateId:           namespace + "/" + second + "/" + moduleProvider,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"import_tags", "resync_triggers"},
			},
		},
	})
}

// TestAccRegistryProvider reads and imports an existing private provider
// without persisting it, and checks the connect refusals. A live create is
// left out: a provider repository has to be named terraform-provider-<type>,
// and the only one the acceptance App sees is already connected and in use.
func TestAccRegistryProvider(t *testing.T) {
	testAccPreCheck(t)
	existing := os.Getenv(EnvAccRegistryProvider)
	repo := os.Getenv(EnvAccVCSRepo)
	if existing == "" || repo == "" {
		t.Skipf("the registry provider acceptance test needs %s and %s", EnvAccRegistryProvider, EnvAccVCSRepo)
	}
	address, ok := splitImportID(existing, 2)
	if !ok {
		t.Fatalf("%s must be namespace/type, got %q", EnvAccRegistryProvider, existing)
	}
	found, err := testAccClient(t).GetRegistryProvider(context.Background(), address[0], address[1])
	if err != nil {
		t.Fatalf("reading %s: %v", existing, err)
	}
	const resourceAddress = "webbpulse_registry_provider.test"
	config := func(identifier string) string {
		return fmt.Sprintf(`
resource "webbpulse_registry_provider" "test" {
  vcs_repo {
    identifier = %q
  }
}
`, identifier)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(repo),
				ExpectError: regexp.MustCompile(`REGISTRY_INVALID_PROVIDER_NAME`),
			},
			{
				Config:      config(found.VCSRepo),
				ExpectError: regexp.MustCompile(`REGISTRY_PROVIDER_EXISTS`),
			},
			{
				Config:             config(found.VCSRepo),
				ResourceName:       resourceAddress,
				ImportState:        true,
				ImportStateId:      existing,
				ImportStatePersist: false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d instances", len(states))
					}
					attrs := states[0].Attributes
					for key, want := range map[string]string{
						"id":                  existing,
						"source":              found.Source,
						"vcs_repo.identifier": found.VCSRepo,
						"import_releases":     "true",
					} {
						if attrs[key] != want {
							return fmt.Errorf("%s = %q, want %q", key, attrs[key], want)
						}
					}
					return nil
				},
			},
			{
				Config: fmt.Sprintf(`
data "webbpulse_registry_provider" "test" {
  namespace = %q
  type      = %q
}
`, found.Namespace, found.Type),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.webbpulse_registry_provider.test", "vcs_repo", found.VCSRepo),
					resource.TestCheckResourceAttrSet("data.webbpulse_registry_provider.test", "published_versions.0"),
					resource.TestCheckResourceAttrSet("data.webbpulse_registry_provider.test", "versions.0.platforms.0.shasum"),
					resource.TestCheckResourceAttr("data.webbpulse_registry_provider.test", "versions.0.protocols.0", "6.0"),
				),
			},
		},
	})
}
