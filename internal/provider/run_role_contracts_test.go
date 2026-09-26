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

// TestRunRoleDataSourceOnlyReads checks repeated reads and failures without a POST fallback.
func TestRunRoleDataSourceOnlyReads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
		connected bool
	}{
		{"connected", 200, `{"connected":true,"account_id":"123456789012","error":null}`, false, true},
		{"unconnected", 200, `{"connected":false,"account_id":null,"error":"Trust policy refused the probe"}`, false, false},
		{"missing role", 400, `{"detail":{"message":"Set run_role_arn","error_code":"RUN_ROLE_MISSING"}}`, true, false},
		{"missing gateway route", 404, `{"message":"Not Found"}`, true, false},
		{"forbidden", 403, `{"message":"Forbidden"}`, true, false},
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
			if diags := config.Set(ctx, &runRoleCheckModel{WorkspaceID: types.StringValue("ws-test")}); diags.HasError() {
				t.Fatal(diags)
			}
			for range 2 {
				resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
				d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config(config)}, &resp)
				if resp.Diagnostics.HasError() != tc.wantError {
					t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
				}
				if !tc.wantError {
					var got runRoleCheckModel
					if diags := resp.State.Get(ctx, &got); diags.HasError() {
						t.Fatal(diags)
					}
					if got.Connected.ValueBool() != tc.connected || got.AccountID.IsNull() == tc.connected || got.Error.IsNull() != tc.connected {
						t.Fatal("check state did not reflect the response")
					}
				}
				if tc.name == "missing role" && !strings.Contains(resp.Diagnostics[0].Detail(), "run_role_arn") {
					t.Fatal("missing role diagnostic omitted the actionable field")
				}
			}
			if calls != 2 {
				t.Fatalf("got %d requests, want exactly two reads", calls)
			}
		})
	}
}
