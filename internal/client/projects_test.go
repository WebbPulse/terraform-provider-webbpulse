package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestProjectWireBodies checks the paths and bodies each project call sends.
func TestProjectWireBodies(t *testing.T) {
	ctx := context.Background()
	renamed := "Platform Core"
	cleared := ""
	for _, tc := range []struct {
		name       string
		call       func(*Client) error
		wantMethod string
		wantPath   string
		wantBody   string
	}{
		{"create", func(c *Client) error {
			_, err := c.CreateProject(ctx, ProjectCreate{Name: "platform"})
			return err
		}, http.MethodPost, "/api/v1/projects", `{"name":"platform","description":""}`},
		{"rename", func(c *Client) error {
			_, err := c.UpdateProject(ctx, "prj-01JABCDEF0123456789ABCDEFG", ProjectUpdate{Name: &renamed})
			return err
		}, http.MethodPatch, "/api/v1/projects/prj-01JABCDEF0123456789ABCDEFG", `{"name":"Platform Core"}`},
		{"clear description", func(c *Client) error {
			_, err := c.UpdateProject(ctx, "prj-01JABCDEF0123456789ABCDEFG", ProjectUpdate{Description: &cleared})
			return err
		}, http.MethodPatch, "/api/v1/projects/prj-01JABCDEF0123456789ABCDEFG", `{"description":""}`},
		{"read default", func(c *Client) error {
			_, err := c.GetProject(ctx, DefaultProjectID)
			return err
		}, http.MethodGet, "/api/v1/projects/prj-default", ``},
		{"delete", func(c *Client) error {
			return c.DeleteProject(ctx, "prj-01JABCDEF0123456789ABCDEFG")
		}, http.MethodDelete, "/api/v1/projects/prj-01JABCDEF0123456789ABCDEFG", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
				}
				var compact json.RawMessage
				if len(body) > 0 && json.Unmarshal(body, &compact) == nil {
					body = compact
				}
				if req.Method != tc.wantMethod || req.URL.Path != tc.wantPath || string(body) != tc.wantBody {
					t.Errorf("got %s %s %s, want %s %s %s", req.Method, req.URL.Path, body, tc.wantMethod, tc.wantPath, tc.wantBody)
				}
				if req.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(`{"project_id":"prj-default","name":"Default Project","is_default":true,"created_at":null,"updated_at":null}`))
			}))
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestGetProjectByName checks an exact match wins over a match ignoring case
// and an unknown name is a 404.
func TestGetProjectByName(t *testing.T) {
	ctx := context.Background()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[` +
			`{"project_id":"prj-default","name":"Default Project","is_default":true,"workspace_count":2},` +
			`{"project_id":"prj-01JABCDEF0123456789ABCDEFA","name":"platform","workspace_count":0},` +
			`{"project_id":"prj-01JABCDEF0123456789ABCDEFB","name":"Platform","workspace_count":1}]}`))
	}))
	for name, want := range map[string]string{
		"Platform":        "prj-01JABCDEF0123456789ABCDEFB",
		"platform":        "prj-01JABCDEF0123456789ABCDEFA",
		"PLATFORM":        "prj-01JABCDEF0123456789ABCDEFA",
		"default project": DefaultProjectID,
	} {
		got, err := c.GetProjectByName(ctx, name)
		if err != nil || got.ProjectID != want {
			t.Errorf("%s: got %v %v, want %s", name, got, err, want)
		}
	}
	got, err := c.GetProjectByName(ctx, "missing")
	if got != nil || !IsNotFound(err) {
		t.Fatalf("an unknown name returned %v %v", got, err)
	}
}

// TestWorkspaceProjectIDOnTheWire checks a create omits an unset project_id
// and an update sends a move, including one back to the default.
func TestWorkspaceProjectIDOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		body any
		want string
	}{
		{"create in default", WorkspaceCreate{Name: "a", Engine: "terraform", EngineVersion: "1.9.8"}, ""},
		{"create in project", WorkspaceCreate{Name: "a", Engine: "terraform", EngineVersion: "1.9.8", ProjectID: "prj-x"}, "prj-x"},
		{"move back", WorkspaceUpdate{ProjectID: func() *string { v := DefaultProjectID; return &v }()}, DefaultProjectID},
	} {
		raw, err := json.Marshal(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		got, present := decoded["project_id"].(string)
		if present != (tc.want != "") || got != tc.want {
			t.Errorf("%s: project_id = %q present %v in %s", tc.name, got, present, raw)
		}
	}
}
