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
	off := false
	var clearedRoles []string
	for _, tc := range []struct {
		name string
		body WorkspaceUpdate
		want string
	}{
		{"omitted", WorkspaceUpdate{}, `{}`},
		{"cleared", WorkspaceUpdate{RunRoleARN: &cleared, WorkingDirectory: &cleared, Description: &cleared}, `{"run_role_arn":null,"working_directory":null,"description":null}`},
		{"assigned", WorkspaceUpdate{RunRoleARN: &assigned, WorkingDirectory: &assigned, Description: &assigned}, `{"run_role_arn":"configured","working_directory":"configured","description":"configured"}`},
		{"vcs disconnected", WorkspaceUpdate{VCSRepo: &cleared, TrackedBranch: &cleared}, `{"vcs_repo":null,"tracked_branch":null}`},
		{"plan roles cleared", WorkspaceUpdate{PlanAssumeRoleARNs: &clearedRoles}, `{"plan_assume_role_arns":null}`},
		{"plan roles assigned", WorkspaceUpdate{PlanAssumeRoleARNs: &[]string{"arn:aws:iam::111122223333:role/reader"}}, `{"plan_assume_role_arns":["arn:aws:iam::111122223333:role/reader"]}`},
		{"auto-apply off", WorkspaceUpdate{AutoApply: &off}, `{"auto_apply":false}`},
		{"vcs settings", WorkspaceUpdate{VCSRepo: &assigned, TriggerPatterns: &[]string{}, SpeculativePlans: &off, FileTriggersEnabled: &off}, `{"vcs_repo":"configured","trigger_patterns":[],"speculative_plans":false,"file_triggers_enabled":false}`},
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

// TestNotificationUpdateFields distinguishes omission, clearing, and assignment
// on the wire, and checks no notification write echoes the URL in an error.
func TestNotificationUpdateFields(t *testing.T) {
	var cleared *string
	token := "synthetic-token"
	assigned := &token
	off := false
	for _, tc := range []struct {
		name string
		body NotificationConfigurationUpdate
		want string
	}{
		{"omitted", NotificationConfigurationUpdate{}, `{}`},
		{"token cleared", NotificationConfigurationUpdate{Token: &cleared}, `{"token":null}`},
		{"token assigned", NotificationConfigurationUpdate{Token: &assigned}, `{"token":"synthetic-token"}`},
		{"triggers emptied", NotificationConfigurationUpdate{Triggers: &[]string{}}, `{"triggers":[]}`},
		{"disabled", NotificationConfigurationUpdate{Enabled: &off}, `{"enabled":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/workspaces/ws-test/notification-configurations/nc-test" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if string(body) != tc.want {
					t.Errorf("body = %s, want %s", body, tc.want)
				}
				_, _ = w.Write([]byte(`{"id":"nc-test","workspace_id":"ws-test"}`))
			}))
			if _, err := c.UpdateNotificationConfiguration(context.Background(), "ws-test", "nc-test", tc.body); err != nil {
				t.Fatal(err)
			}
		})
	}

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"loc":["body","url"],"input":"https://hooks.slack.com/services/synthetic-secret","msg":"bad"}]}`))
	}))
	_, err := c.CreateNotificationConfiguration(context.Background(), "ws-test", NotificationConfigurationCreate{
		Name:            "alerts",
		DestinationType: DestinationSlack,
		URL:             "https://hooks.slack.com/services/synthetic-secret",
	})
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected a 422 diagnostic without the URL, got %v", err)
	}
}
