package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRegistryRoutes checks each registry method's method, path, body and the
// gate header option on the wire.
func TestRegistryRoutes(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name       string
		call       func(*Client) error
		wantMethod string
		wantPath   string
		wantBody   string
	}{
		{"create module", func(c *Client) error {
			_, err := c.CreateModule(context.Background(), ModuleCreate{VCSRepo: "WebbPulse/infra", Name: "network", Provider: "aws", ImportTags: &off})
			return err
		}, http.MethodPost, "/api/v1/registry/modules", `{"vcs_repo":"WebbPulse/infra","name":"network","provider":"aws","import_tags":false}`},
		{"create module derived", func(c *Client) error {
			_, err := c.CreateModule(context.Background(), ModuleCreate{VCSRepo: "WebbPulse/terraform-aws-network"})
			return err
		}, http.MethodPost, "/api/v1/registry/modules", `{"vcs_repo":"WebbPulse/terraform-aws-network"}`},
		{"get module", func(c *Client) error {
			_, err := c.GetModule(context.Background(), "WebbPulse", "network", "aws")
			return err
		}, http.MethodGet, "/api/v1/registry/modules/WebbPulse/network/aws", ""},
		{"resync module", func(c *Client) error {
			_, err := c.ResyncModule(context.Background(), "WebbPulse", "network", "aws")
			return err
		}, http.MethodPost, "/api/v1/registry/modules/WebbPulse/network/aws/resync", ""},
		{"delete module", func(c *Client) error {
			return c.DeleteModule(context.Background(), "WebbPulse", "network", "aws")
		}, http.MethodDelete, "/api/v1/registry/modules/WebbPulse/network/aws", ""},
		{"list modules", func(c *Client) error {
			_, err := c.ListModules(context.Background())
			return err
		}, http.MethodGet, "/api/v1/registry/modules", ""},
		{"create provider", func(c *Client) error {
			_, err := c.CreateRegistryProvider(context.Background(), ProviderCreate{VCSRepo: "WebbPulse/terraform-provider-webbpulse", ImportReleases: &off})
			return err
		}, http.MethodPost, "/api/v1/registry/providers", `{"vcs_repo":"WebbPulse/terraform-provider-webbpulse","import_releases":false}`},
		{"get provider", func(c *Client) error {
			_, err := c.GetRegistryProvider(context.Background(), "WebbPulse", "webbpulse")
			return err
		}, http.MethodGet, "/api/v1/registry/providers/WebbPulse/webbpulse", ""},
		{"resync provider", func(c *Client) error {
			_, err := c.ResyncRegistryProvider(context.Background(), "WebbPulse", "webbpulse")
			return err
		}, http.MethodPost, "/api/v1/registry/providers/WebbPulse/webbpulse/resync", ""},
		{"delete provider", func(c *Client) error {
			return c.DeleteRegistryProvider(context.Background(), "WebbPulse", "webbpulse")
		}, http.MethodDelete, "/api/v1/registry/providers/WebbPulse/webbpulse", ""},
		{"list providers", func(c *Client) error {
			_, err := c.ListRegistryProviders(context.Background())
			return err
		}, http.MethodGet, "/api/v1/registry/providers", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.wantMethod || r.URL.EscapedPath() != tc.wantPath {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.EscapedPath(), tc.wantMethod, tc.wantPath)
				}
				if tc.wantBody != "" {
					var body json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if string(body) != tc.wantBody {
						t.Errorf("body = %s, want %s", body, tc.wantBody)
					}
				}
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestRegistryErrorCodes checks a registry refusal keeps its stable code and
// a 404 reads as not found.
func TestRegistryErrorCodes(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":{"message":"WebbPulse/x/aws was not found.","error_code":"REGISTRY_NOT_FOUND"}}`))
	}))
	_, err := c.GetModule(context.Background(), "WebbPulse", "x", "aws")
	if !IsNotFound(err) || ErrorCode(err) != RegistryNotFoundCode {
		t.Fatalf("err = %v", err)
	}
}

// TestWithHeaderIsSent checks an extra header reaches the server without
// displacing the bearer.
func TestWithHeaderIsSent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-origin-verify") != "gate" || r.Header.Get("Authorization") != "Bearer wpk_test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"modules":[]}`))
	}))
	t.Cleanup(server.Close)
	c, err := New(server.URL, "wpk_test", WithHTTPClient(server.Client()), WithHeader("x-origin-verify", "gate"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListModules(context.Background()); err != nil {
		t.Fatal(err)
	}
}
