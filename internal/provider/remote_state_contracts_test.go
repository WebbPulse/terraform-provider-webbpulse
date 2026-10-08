package provider

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

const (
	testSourcePath   = "/api/v1/workspaces/ws-source"
	testConsumerID   = "ws-consumer"
	testOutputsBody  = `{"workspace_id":"ws-source","state_version_id":"v1","outputs":[{"name":"bucket","type":"string","value":"a-bucket"},{"name":"count","type":"number","value":3},{"name":"tags","type":"object","value":{"env":"staging","zones":["a","b"]}},{"name":"nothing","type":"string","value":null}],"sensitive_output_names":["token","password"]}`
	testNoStateBody  = `{"workspace_id":"ws-source","state_version_id":null,"outputs":[],"sensitive_output_names":[]}`
	testNotSharedErr = `{"detail":{"message":"This workspace does not share its outputs with yours.","error_code":"REMOTE_STATE_NOT_SHARED"}}`
)

// readOutputs runs the outputs data source against one canned response.
func readOutputs(t *testing.T, status int, body string) (workspaceOutputsModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	d := &workspaceOutputsDataSource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != testSourcePath+"/outputs" {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})}
	var schema datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schema)
	config := tfsdk.State{Schema: schema.Schema}
	diags := config.Set(ctx, &workspaceOutputsModel{
		WorkspaceID:          types.StringValue("ws-source"),
		StateVersionID:       types.StringNull(),
		Values:               types.DynamicNull(),
		SensitiveOutputNames: types.ListNull(types.StringType),
	})
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config(config)}, &resp)
	diags.Append(resp.Diagnostics...)
	var got workspaceOutputsModel
	if !diags.HasError() {
		diags.Append(resp.State.Get(ctx, &got)...)
	}
	return got, diags
}

// TestWorkspaceOutputsKeepTypes checks every output lands in values with its
// own type and the sensitive names are listed sorted.
func TestWorkspaceOutputsKeepTypes(t *testing.T) {
	got, diags := readOutputs(t, http.StatusOK, testOutputsBody)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got.StateVersionID.ValueString() != "v1" {
		t.Fatalf("state version = %s", got.StateVersionID)
	}
	object, ok := got.Values.UnderlyingValue().(basetypes.ObjectValue)
	if !ok {
		t.Fatalf("values is %T, want an object", got.Values.UnderlyingValue())
	}
	attributes := object.Attributes()
	if attributes["bucket"].(basetypes.StringValue).ValueString() != "a-bucket" {
		t.Errorf("bucket = %s", attributes["bucket"])
	}
	if attributes["count"].(basetypes.NumberValue).ValueBigFloat().Cmp(big.NewFloat(3)) != 0 {
		t.Errorf("count = %s", attributes["count"])
	}
	tags := attributes["tags"].(basetypes.ObjectValue).Attributes()
	if tags["env"].(basetypes.StringValue).ValueString() != "staging" || len(tags["zones"].(basetypes.TupleValue).Elements()) != 2 {
		t.Errorf("tags = %s", attributes["tags"])
	}
	if !attributes["nothing"].IsNull() {
		t.Errorf("nothing = %s", attributes["nothing"])
	}
	var names []string
	got.SensitiveOutputNames.ElementsAs(context.Background(), &names, false)
	if strings.Join(names, ",") != "password,token" {
		t.Errorf("sensitive names = %v", names)
	}
}

// TestWorkspaceOutputsWithoutState checks a workspace with no state yields an
// empty object and a null version.
func TestWorkspaceOutputsWithoutState(t *testing.T) {
	got, diags := readOutputs(t, http.StatusOK, testNoStateBody)
	if diags.HasError() {
		t.Fatal(diags)
	}
	object, ok := got.Values.UnderlyingValue().(basetypes.ObjectValue)
	if !ok || len(object.Attributes()) != 0 || !got.StateVersionID.IsNull() {
		t.Fatalf("unexpected empty read %+v", got)
	}
}

// TestWorkspaceOutputsNotShared checks a refusal surfaces the API's code.
func TestWorkspaceOutputsNotShared(t *testing.T) {
	_, diags := readOutputs(t, http.StatusForbidden, testNotSharedErr)
	if !diags.HasError() || !strings.Contains(diags[0].Detail(), client.RemoteStateNotSharedCode) {
		t.Fatalf("a refusal did not surface its code: %v", diags)
	}
}

// sharingFixture is an in-memory workspace that records each sharing write.
type sharingFixture struct {
	t        *testing.T
	stored   *client.RemoteStateSharing
	requests []string
	bodies   []string
}

// serve answers one workspace read or sharing write.
func (f *sharingFixture) serve(w http.ResponseWriter, req *http.Request) {
	f.requests = append(f.requests, req.Method)
	if req.URL.Path != testSourcePath {
		f.t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		return
	}
	if f.stored == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"No such workspace."}`))
		return
	}
	if req.Method == http.MethodPatch {
		var body client.RemoteStateSharingUpdate
		raw := json.RawMessage{}
		if err := json.NewDecoder(req.Body).Decode(&raw); err != nil {
			f.t.Fatal(err)
		}
		f.bodies = append(f.bodies, string(raw))
		if err := json.Unmarshal(raw, &body); err != nil {
			f.t.Fatal(err)
		}
		f.stored.GlobalRemoteState = body.GlobalRemoteState
		f.stored.RemoteStateConsumerIDs = body.RemoteStateConsumerIDs
		if len(body.RemoteStateConsumerIDs) == 0 {
			f.stored.RemoteStateConsumerIDs = []string{}
		}
	}
	_ = json.NewEncoder(w).Encode(f.stored)
}

// TestRemoteStateSharingLifecycle checks create and update send both fields,
// refresh follows the API, delete clears sharing, and a gone workspace drops
// the resource.
func TestRemoteStateSharingLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := &sharingFixture{t: t, stored: &client.RemoteStateSharing{WorkspaceID: "ws-source", RemoteStateConsumerIDs: []string{}}}
	r := &remoteStateSharingResource{client: contractClient(t, fixture.serve)}

	model := remoteStateSharingModel{
		WorkspaceID:            types.StringValue("ws-source"),
		GlobalRemoteState:      types.BoolValue(false),
		RemoteStateConsumerIDs: notificationTriggerSet(testConsumerID),
	}
	planned := tfsdk.Plan(resourceState(t, r, &model))
	created := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: planned}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if want := `{"global_remote_state":false,"remote_state_consumer_ids":["ws-consumer"]}`; fixture.bodies[0] != want {
		t.Fatalf("create body = %s, want %s", fixture.bodies[0], want)
	}

	model.GlobalRemoteState = types.BoolValue(true)
	model.RemoteStateConsumerIDs = emptyStringSet()
	updated := resource.UpdateResponse{State: created.State}
	r.Update(ctx, resource.UpdateRequest{State: created.State, Plan: tfsdk.Plan(resourceState(t, r, &model))}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if want := `{"global_remote_state":true,"remote_state_consumer_ids":[]}`; fixture.bodies[1] != want {
		t.Fatalf("update body = %s, want %s", fixture.bodies[1], want)
	}

	fixture.stored.RemoteStateConsumerIDs = []string{"ws-elsewhere"}
	read := resource.ReadResponse{State: updated.State}
	r.Read(ctx, resource.ReadRequest{State: updated.State}, &read)
	var got remoteStateSharingModel
	if diags := read.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if !got.RemoteStateConsumerIDs.Equal(notificationTriggerSet("ws-elsewhere")) || !got.GlobalRemoteState.ValueBool() {
		t.Fatalf("refresh did not follow the API: %+v", got)
	}

	deleted := resource.DeleteResponse{State: read.State}
	r.Delete(ctx, resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if want := `{"global_remote_state":false,"remote_state_consumer_ids":[]}`; fixture.bodies[2] != want {
		t.Fatalf("delete body = %s, want %s", fixture.bodies[2], want)
	}

	fixture.stored = nil
	gone := resource.ReadResponse{State: read.State}
	r.Read(ctx, resource.ReadRequest{State: read.State}, &gone)
	if gone.Diagnostics.HasError() || !gone.State.Raw.IsNull() {
		t.Fatalf("a deleted workspace was not dropped: %v", gone.Diagnostics)
	}
	again := resource.DeleteResponse{State: read.State}
	r.Delete(ctx, resource.DeleteRequest{State: read.State}, &again)
	if again.Diagnostics.HasError() {
		t.Fatalf("a 404 on delete was not treated as gone: %v", again.Diagnostics)
	}
	if got := strings.Join(fixture.requests, " "); got != "PATCH PATCH GET PATCH GET PATCH" {
		t.Fatalf("unexpected requests %s", got)
	}
}
