package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestWorkspaceUpdateMergePatch checks the PATCH body carries only changed
// fields and an explicit null for each clearable attribute removed from config.
func TestWorkspaceUpdateMergePatch(t *testing.T) {
	role := types.StringValue("arn:aws:iam::123456789012:role/example")
	for _, tc := range []struct {
		name string
		edit func(*workspaceModel)
		want string
	}{
		{"attach role", func(m *workspaceModel) { m.RunRoleARN = role }, `{"run_role_arn":"arn:aws:iam::123456789012:role/example"}`},
		{"no change", func(*workspaceModel) {}, `{}`},
		{"engine version", func(m *workspaceModel) { m.EngineVersion = types.StringValue("1.10.0") }, `{"engine_version":"1.10.0"}`},
		{"set description", func(m *workspaceModel) { m.Description = types.StringValue("new") }, `{"description":"new"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := workspaceModel{RunRoleARN: types.StringNull(), Description: types.StringValue(""), WorkingDirectory: types.StringValue("")}
			assertWorkspacePatch(t, prior, tc.edit, tc.want)
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*workspaceModel)
		want string
	}{
		{"clear role", func(m *workspaceModel) { m.RunRoleARN = types.StringNull() }, `{"run_role_arn":null}`},
		{"clear working directory", func(m *workspaceModel) { m.WorkingDirectory = types.StringValue("") }, `{"working_directory":null}`},
		{"clear description", func(m *workspaceModel) { m.Description = types.StringValue("") }, `{"description":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := workspaceModel{RunRoleARN: role, Description: types.StringValue("old"), WorkingDirectory: types.StringValue("infra")}
			assertWorkspacePatch(t, prior, tc.edit, tc.want)
		})
	}
}

// assertWorkspacePatch runs one workspace Update from prior to the edited plan
// and checks the exact PATCH body and that state follows the response.
func assertWorkspacePatch(t *testing.T, prior workspaceModel, edit func(*workspaceModel), want string) {
	t.Helper()
	ctx := context.Background()
	prior.WorkspaceID = types.StringValue("ws-test")
	prior.Name = types.StringValue("example")
	prior.Engine = types.StringValue("terraform")
	prior.EngineVersion = types.StringValue("1.9.8")
	prior.RunRoleSetup = types.ObjectNull(runRoleSetupAttrTypes())
	prior.RunRoleCheckedAt = types.StringValue("old-check")
	prior.RunRoleAccountID = types.StringValue("123456789012")
	next := prior
	edit(&next)

	calls := 0
	r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.Method != http.MethodPatch || req.URL.Path != "/api/v1/workspaces/ws-test" {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		var body json.RawMessage
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body) != want {
			t.Errorf("PATCH body = %s, want %s", body, want)
		}
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: next.EngineVersion.ValueString(),
			RunRoleARN: next.RunRoleARN.ValueStringPointer(), WorkingDirectory: next.WorkingDirectory.ValueString(),
			Description: next.Description.ValueString(), CreatedAt: "created",
		})
	})}
	priorState := resourceState(t, r, &prior)
	planned := resourceState(t, r, &next)
	resp := resource.UpdateResponse{State: priorState}
	r.Update(ctx, resource.UpdateRequest{State: priorState, Plan: tfsdk.Plan(planned)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got workspaceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if calls != 1 || !got.RunRoleARN.Equal(next.RunRoleARN) || !got.Description.Equal(next.Description) ||
		!got.WorkingDirectory.Equal(next.WorkingDirectory) {
		t.Fatal("workspace state did not follow the PATCH response")
	}
}

// TestReadDropsDeletedResources checks a 404 on refresh removes the resource
// from state instead of failing the plan.
func TestReadDropsDeletedResources(t *testing.T) {
	ctx := context.Background()
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not found.","error_code":"NOT_FOUND"}`))
	}

	ws := &workspaceResource{client: contractClient(t, notFound)}
	wsState := resourceState(t, ws, &workspaceModel{WorkspaceID: types.StringValue("ws-test"), RunRoleSetup: types.ObjectNull(runRoleSetupAttrTypes())})
	wsResp := resource.ReadResponse{State: wsState}
	ws.Read(ctx, resource.ReadRequest{State: wsState}, &wsResp)
	if wsResp.Diagnostics.HasError() || !wsResp.State.Raw.IsNull() {
		t.Fatalf("workspace 404 was not dropped from state: %v", wsResp.Diagnostics)
	}

	v := &variableResource{client: contractClient(t, notFound)}
	vState := resourceState(t, v, &variableModel{WorkspaceID: types.StringValue("ws-test"), Key: types.StringValue("example")})
	vResp := resource.ReadResponse{State: vState}
	v.Read(ctx, resource.ReadRequest{State: vState}, &vResp)
	if vResp.Diagnostics.HasError() || !vResp.State.Raw.IsNull() {
		t.Fatalf("variable 404 was not dropped from state: %v", vResp.Diagnostics)
	}
}

// TestVariableCreateRefusesAnExistingKey checks create does not silently
// overwrite a variable through the upserting PUT route.
func TestVariableCreateRefusesAnExistingKey(t *testing.T) {
	ctx := context.Background()
	r := &variableResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Errorf("unexpected %s after finding an existing variable", req.Method)
		}
		_ = json.NewEncoder(w).Encode(client.Variable{WorkspaceID: "ws-test", Key: "example", Category: "terraform", CreatedAt: "created"})
	})}
	planned := resourceState(t, r, &variableModel{WorkspaceID: types.StringValue("ws-test"), Key: types.StringValue("example"), Value: types.StringValue("v"), Category: types.StringValue("terraform"), Sensitive: types.BoolValue(false), Description: types.StringValue("")})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("create over an existing variable was accepted")
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
		if len(methods) == 1 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such variable.","error_code":"NOT_FOUND"}`))
			return
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
	if strings.Join(methods, " ") != "GET PUT PUT GET GET" {
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
