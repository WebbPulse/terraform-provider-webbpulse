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

// fakeWorkspaceAPI serves the workspace routes from memory with the API's
// merge patch and VCS resolution rules, so a real Terraform plan and apply can
// be driven without credentials.
type fakeWorkspaceAPI struct {
	mu         sync.Mutex
	workspaces map[string]map[string]any
}

func (f *fakeWorkspaceAPI) resolve(item map[string]any, repo string, branchGiven bool) bool {
	if strings.EqualFold(repo, "WebbPulse/private") {
		return false
	}
	item["vcs_repo"] = repo
	item["vcs_repository_id"] = "42"
	item["vcs_installation_id"] = "7"
	if !branchGiven {
		item["tracked_branch"] = "main"
	}
	return true
}

func (f *fakeWorkspaceAPI) notInstalled(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(`{"detail":{"message":"The GitHub App is not installed on WebbPulse/private.","error_code":"VCS_REPO_NOT_INSTALLED"}}`))
}

func (f *fakeWorkspaceAPI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := strings.TrimPrefix(req.URL.Path, "/api/v1/workspaces/")
	switch req.Method {
	case http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		item := map[string]any{
			"workspace_id": "ws-fake", "name": body["name"], "engine": "terraform", "engine_version": body["engine_version"],
			"working_directory": "", "description": "", "created_at": "created", "trigger_patterns": []any{},
			"speculative_plans": true, "file_triggers_enabled": true,
		}
		for key, value := range body {
			item[key] = value
		}
		if repo, ok := body["vcs_repo"].(string); ok && !f.resolve(item, repo, body["tracked_branch"] != nil) {
			f.notInstalled(w)
			return
		}
		f.workspaces["ws-fake"] = item
		w.WriteHeader(http.StatusCreated)
		f.render(w, item)
	case http.MethodGet:
		item, ok := f.workspaces[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.render(w, item)
	case http.MethodPatch:
		item := map[string]any{}
		for key, value := range f.workspaces[id] {
			item[key] = value
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		for key, value := range body {
			if value == nil {
				delete(item, key)
				continue
			}
			item[key] = value
		}
		if _, ok := body["vcs_repo"]; ok {
			delete(item, "vcs_repository_id")
			delete(item, "vcs_installation_id")
			if repo, ok := body["vcs_repo"].(string); ok && !f.resolve(item, repo, body["tracked_branch"] != nil) {
				f.notInstalled(w)
				return
			}
		}
		f.workspaces[id] = item
		f.render(w, item)
	case http.MethodDelete:
		delete(f.workspaces, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeWorkspaceAPI) render(w http.ResponseWriter, item map[string]any) {
	out := map[string]any{"run_role_setup": map[string]any{
		"principal_arn": "arn:aws:iam::111122223333:role/runner", "principal_arns": []string{},
		"external_id": item["workspace_id"], "role_name": "run",
	}}
	for key, value := range item {
		out[key] = value
	}
	_ = json.NewEncoder(w).Encode(out)
}

// TestWorkspaceVCSLifecycle drives connect, branch change, disconnect and the
// not installed refusal through real Terraform plans against a fake API.
func TestWorkspaceVCSLifecycle(t *testing.T) {
	api := &fakeWorkspaceAPI{workspaces: map[string]map[string]any{}}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	config := func(body string) string {
		return fmt.Sprintf(`
provider "webbpulse" {
  host  = %q
  token = "synthetic-test-only"
}

resource "webbpulse_workspace" "test" {
  name           = "vcs"
  engine_version = "1.9.8"
%s
}
`, server.URL, body)
	}
	const address = "webbpulse_workspace.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "vcs_repo.identifier"),
					resource.TestCheckResourceAttr(address, "speculative_enabled", "true"),
					resource.TestCheckResourceAttr(address, "file_triggers_enabled", "true"),
					resource.TestCheckResourceAttr(address, "trigger_patterns.#", "0"),
				),
			},
			{
				Config: config(`
  vcs_repo {
    branch = "main"
  }
`),
				ExpectError: regexp.MustCompile(`identifier`),
			},
			{
				Config: config(`
  working_directory = "infra"
  trigger_patterns  = ["/modules/**"]

  vcs_repo {
    identifier = "WebbPulse/infra"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "vcs_repo.identifier", "WebbPulse/infra"),
					resource.TestCheckResourceAttr(address, "vcs_repo.branch", "main"),
					resource.TestCheckResourceAttr(address, "vcs_repo.repository_id", "42"),
					resource.TestCheckResourceAttr(address, "vcs_repo.installation_id", "7"),
					resource.TestCheckResourceAttr(address, "trigger_patterns.0", "/modules/**"),
				),
			},
			{
				Config: config(`
  working_directory     = "infra"
  trigger_patterns      = ["/modules/**"]
  file_triggers_enabled = false
  speculative_enabled   = false

  vcs_repo {
    identifier = "WebbPulse/infra"
    branch     = "release"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "vcs_repo.branch", "release"),
					resource.TestCheckResourceAttr(address, "vcs_repo.repository_id", "42"),
					resource.TestCheckResourceAttr(address, "speculative_enabled", "false"),
					resource.TestCheckResourceAttr(address, "file_triggers_enabled", "false"),
				),
			},
			{
				Config: config(`
  vcs_repo {
    identifier = "WebbPulse/private"
  }
`),
				ExpectError: regexp.MustCompile(`VCS_REPO_NOT_INSTALLED`),
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "vcs_repo.identifier"),
					resource.TestCheckResourceAttr(address, "trigger_patterns.#", "0"),
					resource.TestCheckResourceAttr(address, "speculative_enabled", "true"),
					func(_ *terraform.State) error {
						api.mu.Lock()
						defer api.mu.Unlock()
						for _, key := range []string{"vcs_repo", "tracked_branch", "vcs_repository_id"} {
							if _, ok := api.workspaces["ws-fake"][key]; ok {
								return fmt.Errorf("%s survived the disconnect", key)
							}
						}
						return nil
					},
				),
			},
		},
	})
}
