package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// contractClient confines resource operations to a local HTTP fixture.
func contractClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := client.New(server.URL, "synthetic-test-only", client.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// resourceState encodes a model using the resource's real Terraform schema.
func resourceState(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if diags := state.Set(context.Background(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

// TestWorkspaceUpdateRoleLifecycle checks null PATCHes and the resulting Terraform state.
func TestWorkspaceUpdateRoleLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		old     types.String
		next    types.String
		present bool
	}{
		{"attach", types.StringNull(), types.StringValue("arn:aws:iam::123456789012:role/example"), true},
		{"clear", types.StringValue("arn:aws:iam::123456789012:role/example"), types.StringNull(), true},
		{"unchanged", types.StringValue("arn:aws:iam::123456789012:role/example"), types.StringValue("arn:aws:iam::123456789012:role/example"), false},
		{"still absent", types.StringNull(), types.StringNull(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				calls++
				if req.Method != http.MethodPatch || req.URL.Path != "/api/v1/workspaces/ws-test" {
					t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				value, present := body["run_role_arn"]
				if present != tc.present || (present && tc.next.IsNull() && value != nil) || (present && !tc.next.IsNull() && value != tc.next.ValueString()) {
					t.Errorf("unexpected role PATCH: %v", body)
				}
				_ = json.NewEncoder(w).Encode(client.Workspace{WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", RunRoleARN: tc.next.ValueStringPointer(), CreatedAt: "created"})
			})}
			model := workspaceModel{WorkspaceID: types.StringValue("ws-test"), Name: types.StringValue("example"), Engine: types.StringValue("terraform"), EngineVersion: types.StringValue("1.9.8"), RunRoleARN: tc.old, Description: types.StringValue(""), WorkingDirectory: types.StringValue(""), RunRoleSetup: types.ObjectNull(runRoleSetupAttrTypes()), RunRoleCheckedAt: types.StringValue("old-check"), RunRoleAccountID: types.StringValue("123456789012")}
			prior := resourceState(t, r, &model)
			model.RunRoleARN = tc.next
			planned := resourceState(t, r, &model)
			resp := resource.UpdateResponse{State: prior}
			r.Update(ctx, resource.UpdateRequest{State: prior, Plan: tfsdk.Plan(planned)}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got workspaceModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatal(diags)
			}
			if calls != 1 || !got.RunRoleARN.Equal(tc.next) || !got.RunRoleCheckedAt.IsNull() || !got.RunRoleAccountID.IsNull() {
				t.Fatal("workspace state did not follow the PATCH response")
			}
		})
	}
}

// TestSensitiveVariableLifecycle preserves configured values across redacted writes and reads.
func TestSensitiveVariableLifecycle(t *testing.T) {
	ctx := context.Background()
	var methods []string
	r := &variableResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		methods = append(methods, req.Method)
		if req.URL.Path != "/api/v1/workspaces/ws-test/variables/example" {
			t.Errorf("unexpected path %s", req.URL.Path)
		}
		if req.Method == http.MethodPut {
			var body client.VariableWrite
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !body.Sensitive || body.Value == "" {
				t.Error("write lost its configured value or sensitive flag")
			}
		}
		_ = json.NewEncoder(w).Encode(client.Variable{WorkspaceID: "ws-test", Key: "example", Sensitive: true, Category: "env", CreatedAt: "created"})
	})}
	model := variableModel{WorkspaceID: types.StringValue("ws-test"), Key: types.StringValue("example"), Value: types.StringValue("synthetic-first"), Category: types.StringValue("env"), Sensitive: types.BoolValue(true), Description: types.StringValue("")}
	planned := resourceState(t, r, &model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got variableModel
	if diags := created.State.Get(ctx, &got); diags.HasError() || !got.Value.Equal(model.Value) {
		t.Fatal("create lost the configured value")
	}
	model.Value = types.StringValue("synthetic-replacement")
	planned = resourceState(t, r, &model)
	updated := resource.UpdateResponse{State: created.State}
	r.Update(ctx, resource.UpdateRequest{State: created.State, Plan: tfsdk.Plan(planned)}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	read := resource.ReadResponse{State: updated.State}
	r.Read(ctx, resource.ReadRequest{State: updated.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if diags := read.State.Get(ctx, &got); diags.HasError() || !got.Value.Equal(model.Value) {
		t.Fatal("update or refresh lost the replacement value")
	}
	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ws-test/example"}, &imported)
	if !imported.Diagnostics.HasError() {
		t.Fatal("sensitive import was accepted")
	}
	if len(methods) != 4 || methods[0] != "PUT" || methods[1] != "PUT" || methods[2] != "GET" || methods[3] != "GET" {
		t.Fatalf("unexpected lifecycle requests %v", methods)
	}
}

// TestVariableMapping respects redaction and still detects nonsensitive drift.
func TestVariableMapping(t *testing.T) {
	for _, sensitive := range []bool{true, false} {
		model := variableModel{Value: types.StringValue("configured")}
		returned := "remote"
		applyVariable(&client.Variable{Sensitive: sensitive, Value: &returned}, &model)
		want := "remote"
		if sensitive {
			want = "configured"
		}
		if model.Value.ValueString() != want {
			t.Fatal("value mapping violated sensitivity or drift semantics")
		}
	}
}
