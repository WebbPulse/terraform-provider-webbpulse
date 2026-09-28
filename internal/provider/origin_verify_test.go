package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// gateRecorder routes workspace and registry calls to their fakes and records
// the access gate header each request carried.
type gateRecorder struct {
	mu        sync.Mutex
	seen      []string
	present   []bool
	workspace *fakeWorkspaceAPI
	registry  *fakeRegistryAPI
}

func (g *gateRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	values, ok := req.Header[http.CanonicalHeaderKey(client.OriginVerifyHeader)]
	g.mu.Lock()
	g.present = append(g.present, ok)
	g.seen = append(g.seen, strings.Join(values, ","))
	g.mu.Unlock()
	if strings.HasPrefix(req.URL.Path, "/api/v1/registry/") {
		g.registry.ServeHTTP(w, req)
		return
	}
	g.workspace.ServeHTTP(w, req)
}

func runGateLifecycle(t *testing.T, providerExtra string) *gateRecorder {
	t.Helper()
	recorder := &gateRecorder{
		workspace: &fakeWorkspaceAPI{workspaces: map[string]map[string]any{}},
		registry:  newFakeRegistryAPI(),
	}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)

	config := fmt.Sprintf(`
provider "webbpulse" {
  host  = %q
  token = "synthetic-test-only"
%s
}

resource "webbpulse_workspace" "test" {
  name           = "gate"
  engine_version = "1.9.8"
}

resource "webbpulse_registry_module" "test" {
  name            = "network"
  module_provider = "aws"
  vcs_repo {
    identifier = "WebbPulse/terraform-aws-network"
  }
}
`, server.URL, providerExtra)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    []resource.TestStep{{Config: config}},
	})

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.seen) == 0 {
		t.Fatal("the fake API saw no requests")
	}
	return recorder
}

// TestOriginVerifyFromConfigurationIsSentOnEveryRequest checks the configured
// value reaches workspace and registry routes and wins over the environment.
func TestOriginVerifyFromConfigurationIsSentOnEveryRequest(t *testing.T) {
	t.Setenv(EnvOriginVerify, "synthetic-from-env")
	recorder := runGateLifecycle(t, `  origin_verify = "synthetic-from-config"`)
	for i, value := range recorder.seen {
		if value != "synthetic-from-config" {
			t.Errorf("request %d carried %s %q, want the configured value", i, client.OriginVerifyHeader, value)
		}
	}
}

// TestOriginVerifyFallsBackToTheEnvironment checks the environment variable is
// sent when the attribute is left out.
func TestOriginVerifyFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv(EnvOriginVerify, "synthetic-from-env")
	recorder := runGateLifecycle(t, "")
	for i, value := range recorder.seen {
		if value != "synthetic-from-env" {
			t.Errorf("request %d carried %s %q, want the environment value", i, client.OriginVerifyHeader, value)
		}
	}
}

// TestOriginVerifyUnsetSendsNoHeader checks no header is sent when neither the
// attribute nor the environment variable is set.
func TestOriginVerifyUnsetSendsNoHeader(t *testing.T) {
	t.Setenv(EnvOriginVerify, "")
	recorder := runGateLifecycle(t, "")
	for i, present := range recorder.present {
		if present {
			t.Errorf("request %d carried %s although no value was set", i, client.OriginVerifyHeader)
		}
	}
}
