package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	testProjectID   = "prj-01JABCDEF0123456789ABCDEFG"
	testProjectPath = "/api/v1/projects"
)

// projectFixture is an in-memory projects API that records each request's
// method and raw body.
type projectFixture struct {
	t        *testing.T
	stored   *client.Project
	requests []string
	bodies   []string
}

// serve answers one request against the stored project.
func (f *projectFixture) serve(w http.ResponseWriter, req *http.Request) {
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
	itemPath := testProjectPath + "/" + testProjectID
	switch {
	case req.Method == http.MethodPost && req.URL.Path == testProjectPath:
		created := "created"
		f.stored = &client.Project{ProjectID: testProjectID, CreatedAt: &created, UpdatedAt: &created}
		f.patch(raw)
		w.WriteHeader(http.StatusCreated)
	case req.URL.Path != itemPath:
		f.t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		return
	case f.stored == nil:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such project."}`))
		return
	case req.Method == http.MethodPatch:
		f.patch(raw)
		updated := "updated"
		f.stored.UpdatedAt = &updated
	case req.Method == http.MethodDelete:
		f.stored = nil
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(f.stored)
}

// patch applies a create or edit body to the stored project.
func (f *projectFixture) patch(raw map[string]any) {
	if v, ok := raw["name"].(string); ok {
		f.stored.Name = v
	}
	if v, ok := raw["description"].(string); ok {
		f.stored.Description = v
	}
}

// TestProjectLifecycle checks create sends the name and description, each
// update sends only the changed fields, and delete then refresh drops the
// resource while a second delete treats the 404 as gone.
func TestProjectLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := &projectFixture{t: t}
	r := &projectResource{client: contractClient(t, fixture.serve)}

	planned := tfsdk.Plan(resourceState(t, r, &projectModel{
		ID:             types.StringUnknown(),
		Name:           types.StringValue("platform"),
		Description:    types.StringValue("shared infrastructure"),
		WorkspaceCount: types.Int64Unknown(),
		CreatedAt:      types.StringUnknown(),
		UpdatedAt:      types.StringUnknown(),
	}))
	created := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: planned}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if want := `{"name":"platform","description":"shared infrastructure"}`; fixture.bodies[0] != want {
		t.Fatalf("POST body = %s, want %s", fixture.bodies[0], want)
	}

	read := resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	var got projectModel
	if diags := read.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.ID.ValueString() != testProjectID || got.Name.ValueString() != "platform" ||
		got.WorkspaceCount.ValueInt64() != 0 || got.CreatedAt.ValueString() != "created" {
		t.Fatalf("unexpected refreshed state %+v", got)
	}

	state := read.State
	for _, step := range []struct {
		name string
		edit func(*projectModel)
		want string
	}{
		{"rename", func(m *projectModel) { m.Name = types.StringValue("Platform Core") }, `{"name":"Platform Core"}`},
		{"clear description", func(m *projectModel) { m.Description = types.StringValue("") }, `{"description":""}`},
	} {
		var prior projectModel
		if diags := state.Get(ctx, &prior); diags.HasError() {
			t.Fatal(diags)
		}
		next := prior
		step.edit(&next)
		next.WorkspaceCount, next.UpdatedAt = types.Int64Unknown(), types.StringUnknown()
		updated := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan(resourceState(t, r, &next))}, &updated)
		if updated.Diagnostics.HasError() {
			t.Fatalf("%s: %v", step.name, updated.Diagnostics)
		}
		if body := fixture.bodies[len(fixture.bodies)-1]; body != step.want {
			t.Fatalf("%s: PATCH body = %s, want %s", step.name, body, step.want)
		}
		if diags := updated.State.Get(ctx, &got); diags.HasError() || !got.Name.Equal(next.Name) ||
			!got.Description.Equal(next.Description) || got.UpdatedAt.ValueString() != "updated" {
			t.Fatalf("%s: state did not follow the plan and the response: %+v", step.name, got)
		}
		state = updated.State
	}

	deleted := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	refreshed := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &refreshed)
	if refreshed.Diagnostics.HasError() || !refreshed.State.Raw.IsNull() {
		t.Fatalf("a deleted project was not dropped from state: %v", refreshed.Diagnostics)
	}
	again := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &again)
	if again.Diagnostics.HasError() {
		t.Fatalf("a 404 on delete was not treated as gone: %v", again.Diagnostics)
	}
	if got := strings.Join(fixture.requests, " "); got != "POST GET PATCH PATCH DELETE GET DELETE" {
		t.Fatalf("unexpected requests %s", got)
	}
}

// refusal answers every request with one API refusal carrying a stable code.
func refusal(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"detail":{"message":"Refused.","error_code":"` + code + `"}}`))
	}
}

// TestProjectRefusalsNameTheirCode checks each project refusal surfaces as a
// diagnostic naming the API's code, a taken name on the name attribute.
func TestProjectRefusalsNameTheirCode(t *testing.T) {
	ctx := context.Background()
	model := projectModel{
		ID:             types.StringValue(testProjectID),
		Name:           types.StringValue("platform"),
		Description:    types.StringValue(""),
		WorkspaceCount: types.Int64Value(1),
		CreatedAt:      types.StringValue("created"),
		UpdatedAt:      types.StringValue("created"),
	}

	r := &projectResource{client: contractClient(t, refusal(http.StatusConflict, client.ProjectNameTakenCode))}
	state := resourceState(t, r, &model)
	created := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(state)}, &created)
	assertCodeDiagnostic(t, created.Diagnostics, client.ProjectNameTakenCode, path.Root("name"))

	for _, code := range []string{client.ProjectNotEmptyCode, client.DefaultProjectReadOnlyCode} {
		r := &projectResource{client: contractClient(t, refusal(http.StatusConflict, code))}
		deleted := resource.DeleteResponse{State: state}
		r.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
		assertCodeDiagnostic(t, deleted.Diagnostics, code, path.Empty())
	}
}

// assertCodeDiagnostic checks a single error diagnostic names the code and,
// when want is not empty, sits on that attribute.
func assertCodeDiagnostic(t *testing.T, diags diag.Diagnostics, code string, want path.Path) {
	t.Helper()
	errs := diags.Errors()
	if len(errs) != 1 {
		t.Fatalf("%s: got %d errors, want 1: %v", code, len(errs), diags)
	}
	if !strings.Contains(errs[0].Detail(), code) {
		t.Errorf("%s: the diagnostic does not name the code: %s", code, errs[0].Detail())
	}
	if want.Equal(path.Empty()) {
		return
	}
	withPath, ok := errs[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(want) {
		t.Errorf("%s: the diagnostic is not on %s", code, want)
	}
}

// TestProjectImportRefusesTheDefault checks the default project cannot be
// imported and any other id passes through without a request.
func TestProjectImportRefusesTheDefault(t *testing.T) {
	ctx := context.Background()
	r := &projectResource{client: contractClient(t, func(_ http.ResponseWriter, req *http.Request) {
		t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
	})}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	emptyState := func() tfsdk.State {
		return tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil)}
	}

	refused := resource.ImportStateResponse{State: emptyState()}
	r.ImportState(ctx, resource.ImportStateRequest{ID: client.DefaultProjectID}, &refused)
	if !refused.Diagnostics.HasError() {
		t.Fatal("the default project was imported")
	}

	imported := resource.ImportStateResponse{State: emptyState()}
	r.ImportState(ctx, resource.ImportStateRequest{ID: testProjectID}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	var id types.String
	if diags := imported.State.GetAttribute(ctx, path.Root("id"), &id); diags.HasError() || id.ValueString() != testProjectID {
		t.Fatalf("imported id = %v, want %s", id, testProjectID)
	}
}

// TestProjectDataSourceLooksUpByName checks a lookup matches ignoring case,
// finds the default project and refuses an unknown name.
func TestProjectDataSourceLooksUpByName(t *testing.T) {
	ctx := context.Background()
	created := "created"
	d := &projectDataSource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != testProjectPath {
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.ProjectList{Items: []client.Project{
			{ProjectID: client.DefaultProjectID, Name: "Default Project", IsDefault: true, WorkspaceCount: 3},
			{ProjectID: testProjectID, Name: "Platform", Description: "shared", WorkspaceCount: 2, CreatedAt: &created, UpdatedAt: &created},
		}})
	})}
	var schema datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schema)

	lookup := func(name string) (projectDataModel, diag.Diagnostics) {
		state := tfsdk.State{Schema: schema.Schema}
		diags := state.Set(ctx, &projectDataModel{Name: types.StringValue(name)})
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
		d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config(state)}, &resp)
		diags.Append(resp.Diagnostics...)
		var got projectDataModel
		if !diags.HasError() {
			diags.Append(resp.State.Get(ctx, &got)...)
		}
		return got, diags
	}

	got, diags := lookup("platform")
	if diags.HasError() || got.ID.ValueString() != testProjectID || got.IsDefault.ValueBool() || got.WorkspaceCount.ValueInt64() != 2 {
		t.Fatalf("lookup ignoring case: %+v %v", got, diags)
	}
	got, diags = lookup("Default Project")
	if diags.HasError() || got.ID.ValueString() != client.DefaultProjectID || !got.IsDefault.ValueBool() || !got.CreatedAt.IsNull() {
		t.Fatalf("default lookup: %+v %v", got, diags)
	}
	if _, diags = lookup("missing"); !diags.HasError() {
		t.Fatal("an unknown name was found")
	}
}

// TestWorkspaceProjectID checks a create in the default project omits
// project_id and reads an API that predates projects as the default, a create
// elsewhere sends it, a move PATCHes it, and an unknown project surfaces
// PROJECT_NOT_FOUND on the attribute.
func TestWorkspaceProjectID(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		projectID string
		wantSent  bool
	}{
		{client.DefaultProjectID, false},
		{testProjectID, true},
	} {
		var sent map[string]any
		r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
			if err := json.NewDecoder(req.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
			projectID, _ := sent["project_id"].(string)
			_ = json.NewEncoder(w).Encode(client.Workspace{
				WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8",
				CreatedAt: "created", ProjectID: projectID,
			})
		})}
		planned := tfsdk.Plan(resourceState(t, r, &workspaceModel{
			Name:          types.StringValue("example"),
			Engine:        types.StringValue("terraform"),
			EngineVersion: types.StringValue("1.9.8"),
			ProjectID:     types.StringValue(tc.projectID),
		}))
		created := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: planned}, &created)
		if created.Diagnostics.HasError() {
			t.Fatal(created.Diagnostics)
		}
		if _, ok := sent["project_id"]; ok != tc.wantSent {
			t.Errorf("%s: project_id sent = %v, want %v", tc.projectID, ok, tc.wantSent)
		}
		var got workspaceModel
		if diags := created.State.Get(ctx, &got); diags.HasError() || got.ProjectID.ValueString() != tc.projectID {
			t.Errorf("%s: state project_id = %v", tc.projectID, got.ProjectID)
		}
	}

	prior := workspaceModel{RunRoleARN: types.StringNull(), Description: types.StringValue(""), WorkingDirectory: types.StringValue(""), ProjectID: types.StringValue(client.DefaultProjectID)}
	assertWorkspacePatch(t, prior, func(m *workspaceModel) { m.ProjectID = types.StringValue(testProjectID) }, `{"project_id":"`+testProjectID+`"}`)
	prior.ProjectID = types.StringValue(testProjectID)
	assertWorkspacePatch(t, prior, func(m *workspaceModel) { m.ProjectID = types.StringValue(client.DefaultProjectID) }, `{"project_id":"prj-default"}`)

	r := &workspaceResource{client: contractClient(t, refusal(http.StatusUnprocessableEntity, client.ProjectNotFoundCode))}
	prior.WorkspaceID, prior.Name, prior.Engine, prior.EngineVersion = types.StringValue("ws-test"), types.StringValue("example"), types.StringValue("terraform"), types.StringValue("1.9.8")
	priorState := resourceState(t, r, &prior)
	next := prior
	next.ProjectID = types.StringValue("prj-01JZZZZZZZZZZZZZZZZZZZZZZZ")
	resp := resource.UpdateResponse{State: priorState}
	r.Update(ctx, resource.UpdateRequest{State: priorState, Plan: tfsdk.Plan(resourceState(t, r, &next))}, &resp)
	assertCodeDiagnostic(t, resp.Diagnostics, client.ProjectNotFoundCode, path.Root("project_id"))
}
