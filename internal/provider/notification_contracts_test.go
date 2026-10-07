package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testNotificationID   = "nc-01JABCDEF0123456789ABCDEFG"
	testNotificationPath = "/api/v1/workspaces/ws-test/notification-configurations"
)

// notificationFixture is an in-memory notification API that records each
// request's method and raw body.
type notificationFixture struct {
	t        *testing.T
	stored   *client.NotificationConfiguration
	requests []string
	bodies   []string
}

// serve answers one request against the stored configuration.
func (f *notificationFixture) serve(w http.ResponseWriter, req *http.Request) {
	f.requests = append(f.requests, req.Method)
	var raw map[string]any
	if req.Body != nil && req.ContentLength != 0 {
		var body json.RawMessage
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		f.bodies = append(f.bodies, string(body))
		if err := json.Unmarshal(body, &raw); err != nil {
			f.t.Error(err)
		}
	}
	itemPath := testNotificationPath + "/" + testNotificationID
	switch {
	case req.Method == http.MethodPost && req.URL.Path == testNotificationPath:
		f.stored = &client.NotificationConfiguration{
			ID: testNotificationID, WorkspaceID: "ws-test", CreatedAt: "created", UpdatedAt: "created",
		}
		f.patch(raw)
		w.WriteHeader(http.StatusCreated)
	case req.URL.Path != itemPath:
		f.t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		return
	case f.stored == nil:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such notification configuration."}`))
		return
	case req.Method == http.MethodPatch:
		f.patch(raw)
		f.stored.UpdatedAt = "updated"
	case req.Method == http.MethodDelete:
		f.stored = nil
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(f.stored)
}

// patch applies a create or edit body to the stored configuration, masking the URL as the API does.
func (f *notificationFixture) patch(raw map[string]any) {
	if v, ok := raw["name"].(string); ok {
		f.stored.Name = v
	}
	if v, ok := raw["destination_type"].(string); ok {
		f.stored.DestinationType = v
	}
	if v, ok := raw["url"].(string); ok {
		f.stored.URLMasked = v[:strings.Index(v[len("https://"):], "/")+len("https://")] + "/****"
	}
	if v, ok := raw["token"]; ok {
		f.stored.HasToken = v != nil && v != ""
	}
	if v, ok := raw["enabled"].(bool); ok {
		f.stored.Enabled = v
	}
	if v, ok := raw["triggers"].([]any); ok {
		f.stored.Triggers = []string{}
		for _, trigger := range v {
			f.stored.Triggers = append(f.stored.Triggers, trigger.(string))
		}
	}
}

// notificationTriggerSet builds a triggers set value.
func notificationTriggerSet(triggers ...string) types.Set {
	values := make([]attr.Value, 0, len(triggers))
	for _, trigger := range triggers {
		values = append(values, types.StringValue(trigger))
	}
	return types.SetValueMust(types.StringType, values)
}

// notificationPlan encodes a model as a plan using the resource schema.
func notificationPlan(t *testing.T, r resource.Resource, model notificationConfigurationModel) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan(resourceState(t, r, &model))
}

// TestNotificationConfigurationLifecycle checks create sends the URL and token,
// state keeps them though no response carries them, each update sends only the
// changed fields, and delete then refresh drops the resource.
func TestNotificationConfigurationLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := &notificationFixture{t: t}
	r := &notificationConfigurationResource{client: contractClient(t, fixture.serve)}

	model := notificationConfigurationModel{
		ID:              types.StringUnknown(),
		WorkspaceID:     types.StringValue("ws-test"),
		Name:            types.StringValue("alerts"),
		DestinationType: types.StringValue("generic"),
		URL:             types.StringValue("https://example.invalid/synthetic-hook"),
		Token:           types.StringValue("synthetic-token"),
		Enabled:         types.BoolValue(true),
		Triggers:        notificationTriggerSet("run:errored", "run:completed"),
		URLMasked:       types.StringUnknown(),
		HasToken:        types.BoolUnknown(),
		CreatedAt:       types.StringUnknown(),
		UpdatedAt:       types.StringUnknown(),
	}
	planned := notificationPlan(t, r, model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: planned}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	wantCreate := `{"name":"alerts","destination_type":"generic","url":"https://example.invalid/synthetic-hook","token":"synthetic-token","enabled":true,"triggers":["run:completed","run:errored"]}`
	if fixture.bodies[0] != wantCreate {
		t.Fatalf("POST body = %s, want %s", fixture.bodies[0], wantCreate)
	}

	read := resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	var got notificationConfigurationModel
	if diags := read.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.ID.ValueString() != testNotificationID || !got.URL.Equal(model.URL) || !got.Token.Equal(model.Token) ||
		got.URLMasked.ValueString() != "https://example.invalid/****" || !got.HasToken.ValueBool() {
		t.Fatalf("refresh lost the write-only values or the computed ones: %+v", got)
	}

	state := read.State
	for _, step := range []struct {
		name string
		edit func(*notificationConfigurationModel)
		want string
	}{
		{"url only", func(m *notificationConfigurationModel) {
			m.URL = types.StringValue("https://example.invalid/synthetic-other")
		}, `{"url":"https://example.invalid/synthetic-other"}`},
		{"token removed", func(m *notificationConfigurationModel) { m.Token = types.StringNull() }, `{"token":null}`},
		{"destination and triggers", func(m *notificationConfigurationModel) {
			m.DestinationType = types.StringValue("slack")
			m.Triggers = notificationTriggerSet("run:created")
		}, `{"destination_type":"slack","url":"https://example.invalid/synthetic-other","triggers":["run:created"]}`},
		{"disabled and triggers emptied", func(m *notificationConfigurationModel) {
			m.Enabled = types.BoolValue(false)
			m.Triggers = notificationTriggerSet()
		}, `{"enabled":false,"triggers":[]}`},
	} {
		var prior notificationConfigurationModel
		if diags := state.Get(ctx, &prior); diags.HasError() {
			t.Fatal(diags)
		}
		next := prior
		step.edit(&next)
		next.URLMasked, next.HasToken, next.UpdatedAt = types.StringUnknown(), types.BoolUnknown(), types.StringUnknown()
		updated := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{State: state, Plan: notificationPlan(t, r, next)}, &updated)
		if updated.Diagnostics.HasError() {
			t.Fatalf("%s: %v", step.name, updated.Diagnostics)
		}
		if body := fixture.bodies[len(fixture.bodies)-1]; body != step.want {
			t.Fatalf("%s: PATCH body = %s, want %s", step.name, body, step.want)
		}
		if diags := updated.State.Get(ctx, &got); diags.HasError() || !got.URL.Equal(next.URL) || !got.Token.Equal(next.Token) ||
			!got.Triggers.Equal(next.Triggers) || got.UpdatedAt.ValueString() != "updated" {
			t.Fatalf("%s: state did not follow the plan and the response: %+v", step.name, got)
		}
		state = updated.State
	}
	if got.HasToken.ValueBool() || got.DestinationType.ValueString() != "slack" || got.Enabled.ValueBool() {
		t.Fatalf("final state does not reflect the edits: %+v", got)
	}

	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	refreshed := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &refreshed)
	if refreshed.Diagnostics.HasError() || !refreshed.State.Raw.IsNull() {
		t.Fatalf("a deleted configuration was not dropped from state: %v", refreshed.Diagnostics)
	}
	again := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &again)
	if again.Diagnostics.HasError() {
		t.Fatalf("a 404 on delete was not treated as gone: %v", again.Diagnostics)
	}
	if got := strings.Join(fixture.requests, " "); got != "POST GET PATCH PATCH PATCH PATCH DELETE GET DELETE" {
		t.Fatalf("unexpected requests %s", got)
	}
}

// TestNotificationConfigurationImport checks import takes
// <workspace_id>/<notification_id>, leaves the write-only values null and
// refuses a malformed id without calling the API.
func TestNotificationConfigurationImport(t *testing.T) {
	ctx := context.Background()
	fixture := &notificationFixture{t: t, stored: &client.NotificationConfiguration{
		ID:              testNotificationID,
		WorkspaceID:     "ws-test",
		Name:            "alerts",
		DestinationType: "discord",
		Enabled:         true,
		Triggers:        []string{"run:errored"},
		URLMasked:       "https://discord.com/****",
		CreatedAt:       "created",
		UpdatedAt:       "updated",
	}}
	r := &notificationConfigurationResource{client: contractClient(t, fixture.serve)}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)

	imported := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ws-test/" + testNotificationID}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	var got notificationConfigurationModel
	if diags := imported.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.ID.ValueString() != testNotificationID || got.DestinationType.ValueString() != "discord" ||
		!got.URL.IsNull() || !got.Token.IsNull() || !got.Triggers.Equal(notificationTriggerSet("run:errored")) {
		t.Fatalf("unexpected imported state %+v", got)
	}

	for _, id := range []string{testNotificationID, "ws-test/", "/" + testNotificationID} {
		bad := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, &bad)
		if !bad.Diagnostics.HasError() {
			t.Errorf("import id %q was accepted", id)
		}
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("got %d requests, want only the one import read", len(fixture.requests))
	}
}

// TestNotificationConfigurationTokenNeedsGeneric checks a token on a Slack or
// Discord destination is refused at validation, before any request.
func TestNotificationConfigurationTokenNeedsGeneric(t *testing.T) {
	ctx := context.Background()
	r := &notificationConfigurationResource{}
	for _, tc := range []struct {
		destination string
		token       types.String
		wantError   bool
	}{
		{"slack", types.StringValue("synthetic-token"), true},
		{"discord", types.StringValue("synthetic-token"), true},
		{"generic", types.StringValue("synthetic-token"), false},
		{"slack", types.StringNull(), false},
	} {
		state := resourceState(t, r, &notificationConfigurationModel{
			WorkspaceID:     types.StringValue("ws-test"),
			Name:            types.StringValue("alerts"),
			DestinationType: types.StringValue(tc.destination),
			URL:             types.StringValue("https://example.invalid/synthetic"),
			Token:           tc.token,
			Enabled:         types.BoolValue(true),
			Triggers:        notificationTriggerSet(),
		})
		resp := resource.ValidateConfigResponse{}
		r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: state.Raw}}, &resp)
		if resp.Diagnostics.HasError() != tc.wantError {
			t.Errorf("%s with token %v: error = %v, want %v", tc.destination, tc.token, resp.Diagnostics.HasError(), tc.wantError)
		}
	}
}
