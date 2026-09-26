package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	connectedCheck  = `{"connected":true,"status":"connected","account_id":"123456789012","error":null,"run_id":"run-01J","checked_at":"2026-09-25T12:00:00Z"}`
	failedCheck     = `{"connected":false,"status":"failed","account_id":null,"error":"Trust policy refused the runner","run_id":"run-01J","checked_at":"2026-09-25T12:00:00Z"}`
	unverifiedCheck = `{"connected":false,"status":"unverified","account_id":null,"error":null,"run_id":null,"checked_at":null}`
)

// TestRunRoleDataSourceOnlyReads checks repeated reads and failures without a POST fallback.
func TestRunRoleDataSourceOnlyReads(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		failIfNot  bool
		wantError  string
		wantStatus string
	}{
		{"connected", 200, connectedCheck, false, "", "connected"},
		{"connected strict", 200, connectedCheck, true, "", "connected"},
		{"failed", 200, failedCheck, false, "", "failed"},
		{"unverified", 200, unverifiedCheck, false, "", "unverified"},
		{"failed strict", 200, failedCheck, true, "Trust policy refused the runner", ""},
		{"unverified strict", 200, unverifiedCheck, true, "plan-only run", ""},
		{"missing role", 400, `{"detail":{"message":"Set run_role_arn","error_code":"RUN_ROLE_MISSING"}}`, false, "run_role_arn", ""},
		{"missing gateway route", 404, `{"message":"Not Found"}`, false, "404", ""},
		{"forbidden", 403, `{"message":"Forbidden"}`, false, "403", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			d := &runRoleCheckDataSource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				calls++
				if req.Method != http.MethodGet || req.URL.Path != "/api/v1/workspaces/ws-test/run-role/check" {
					t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})}
			var schema datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &schema)
			config := tfsdk.State{Schema: schema.Schema}
			model := runRoleCheckModel{WorkspaceID: types.StringValue("ws-test")}
			if tc.failIfNot {
				model.FailIfNotConnected = types.BoolValue(true)
			}
			if diags := config.Set(ctx, &model); diags.HasError() {
				t.Fatal(diags)
			}
			for range 2 {
				resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
				d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config(config)}, &resp)
				if tc.wantError != "" {
					if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics[0].Detail(), tc.wantError) {
						t.Fatalf("diagnostics %v do not carry %q", resp.Diagnostics, tc.wantError)
					}
					continue
				}
				if resp.Diagnostics.HasError() {
					t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
				}
				var got runRoleCheckModel
				if diags := resp.State.Get(ctx, &got); diags.HasError() {
					t.Fatal(diags)
				}
				connected := tc.wantStatus == "connected"
				verified := tc.wantStatus != "unverified"
				if got.Status.ValueString() != tc.wantStatus || got.Connected.ValueBool() != connected ||
					got.AccountID.IsNull() == connected || got.RunID.IsNull() == verified ||
					got.CheckedAt.IsNull() == verified || got.Error.IsNull() != (tc.wantStatus != "failed") {
					t.Fatalf("check state did not reflect the response: %+v", got)
				}
			}
			if calls != 2 {
				t.Fatalf("got %d requests, want exactly two reads", calls)
			}
		})
	}
}
