package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestWorkspacePatchNullableFields distinguishes omission, clearing, and assignment on the wire.
func TestWorkspacePatchNullableFields(t *testing.T) {
	var cleared *string
	value := "configured"
	assigned := &value
	for _, tc := range []struct {
		name string
		body WorkspaceUpdate
		want string
	}{
		{"omitted", WorkspaceUpdate{}, `{}`},
		{"cleared", WorkspaceUpdate{RunRoleARN: &cleared, WorkingDirectory: &cleared, Description: &cleared}, `{"run_role_arn":null,"working_directory":null,"description":null}`},
		{"assigned", WorkspaceUpdate{RunRoleARN: &assigned, WorkingDirectory: &assigned, Description: &assigned}, `{"run_role_arn":"configured","working_directory":"configured","description":"configured"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s", r.Method)
				}
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if string(body) != tc.want {
					t.Errorf("body = %s, want %s", body, tc.want)
				}
				_, _ = w.Write([]byte(`{"workspace_id":"ws-test"}`))
			}))
			if _, err := c.UpdateWorkspace(context.Background(), "ws-test", tc.body); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestErrorBodiesDoNotExposeSubmittedValues excludes raw validation and proxy payloads.
func TestErrorBodiesDoNotExposeSubmittedValues(t *testing.T) {
	for _, body := range []string{
		`{"detail":[{"loc":["body","value"],"input":"synthetic-private-value","msg":"too long"}]}`,
		`proxy echoed synthetic-private-value`,
		`{"value":"synthetic-private-value"}`,
	} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Request-ID", "req-validation")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(body))
		}))
		_, err := c.PutVariable(context.Background(), "ws-test", "example", VariableWrite{Value: "synthetic-private-value", Sensitive: true})
		if err == nil || strings.Contains(err.Error(), "synthetic-private-value") {
			t.Fatal("expected a diagnostic without the submitted value")
		}
		if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "req-validation") {
			t.Fatal("diagnostic lost its status or request id")
		}
	}
}
