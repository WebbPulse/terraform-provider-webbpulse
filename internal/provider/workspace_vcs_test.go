package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func strPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func vcsObject(t *testing.T, identifier, branch, repositoryID, installationID types.String) types.Object {
	t.Helper()
	object, diags := types.ObjectValueFrom(context.Background(), vcsRepoAttrTypes(), vcsRepoFields{
		Identifier: identifier, Branch: branch, RepositoryID: repositoryID, InstallationID: installationID,
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	return object
}

func connectedVCS(t *testing.T, identifier, branch string) types.Object {
	t.Helper()
	return vcsObject(t, types.StringValue(identifier), types.StringValue(branch), types.StringValue("42"), types.StringValue("7"))
}

func plannedVCS(t *testing.T, identifier string, branch types.String) types.Object {
	t.Helper()
	return vcsObject(t, types.StringValue(identifier), branch, types.StringUnknown(), types.StringUnknown())
}

func stringList(t *testing.T, values ...string) types.List {
	t.Helper()
	list, diags := types.ListValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return list
}

func baseWorkspaceModel(t *testing.T) workspaceModel {
	t.Helper()
	return workspaceModel{
		WorkspaceID:         types.StringValue("ws-test"),
		Name:                types.StringValue("example"),
		Engine:              types.StringValue("terraform"),
		EngineVersion:       types.StringValue("1.9.8"),
		RunRoleARN:          types.StringNull(),
		WorkingDirectory:    types.StringValue(""),
		Description:         types.StringValue(""),
		CreatedAt:           types.StringValue("created"),
		ForceDelete:         types.BoolValue(false),
		VCSRepo:             types.ObjectNull(vcsRepoAttrTypes()),
		TriggerPatterns:     stringList(t),
		FileTriggersEnabled: types.BoolValue(true),
		SpeculativeEnabled:  types.BoolValue(true),
	}
}

// TestWorkspaceVCSUpdatePatch checks the PATCH body for each VCS edit and that
// state follows the response, including the API resolved branch and ids.
func TestWorkspaceVCSUpdatePatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    func(*workspaceModel)
		plan     func(*workspaceModel)
		want     string
		response client.Workspace
		check    func(*testing.T, workspaceModel)
	}{
		{
			name: "connect with the default branch",
			plan: func(m *workspaceModel) { m.VCSRepo = plannedVCS(t, "WebbPulse/infra", types.StringUnknown()) },
			want: `{"vcs_repo":"WebbPulse/infra"}`,
			response: client.Workspace{
				VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("main"),
				VCSRepositoryID: strPtr("42"), VCSInstallationID: strPtr("7"),
			},
			check: func(t *testing.T, m workspaceModel) {
				if !m.VCSRepo.Equal(connectedVCS(t, "WebbPulse/infra", "main")) {
					t.Errorf("vcs_repo = %v, want the resolved connection", m.VCSRepo)
				}
			},
		},
		{
			name:     "connect with a branch",
			plan:     func(m *workspaceModel) { m.VCSRepo = plannedVCS(t, "WebbPulse/infra", types.StringValue("release")) },
			want:     `{"vcs_repo":"WebbPulse/infra","tracked_branch":"release"}`,
			response: client.Workspace{VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("release")},
		},
		{
			name:  "change the branch",
			prior: func(m *workspaceModel) { m.VCSRepo = connectedVCS(t, "WebbPulse/infra", "main") },
			plan: func(m *workspaceModel) {
				m.VCSRepo = vcsObject(t, types.StringValue("WebbPulse/infra"), types.StringValue("dev"), types.StringValue("42"), types.StringValue("7"))
			},
			want:     `{"tracked_branch":"dev"}`,
			response: client.Workspace{VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("dev"), VCSRepositoryID: strPtr("42"), VCSInstallationID: strPtr("7")},
		},
		{
			name:     "move to another repository keeping the branch",
			prior:    func(m *workspaceModel) { m.VCSRepo = connectedVCS(t, "WebbPulse/infra", "main") },
			plan:     func(m *workspaceModel) { m.VCSRepo = plannedVCS(t, "WebbPulse/other", types.StringValue("main")) },
			want:     `{"vcs_repo":"WebbPulse/other","tracked_branch":"main"}`,
			response: client.Workspace{VCSRepo: strPtr("WebbPulse/other"), TrackedBranch: strPtr("main")},
		},
		{
			name:  "disconnect",
			prior: func(m *workspaceModel) { m.VCSRepo = connectedVCS(t, "WebbPulse/infra", "main") },
			plan:  func(m *workspaceModel) { m.VCSRepo = types.ObjectNull(vcsRepoAttrTypes()) },
			want:  `{"vcs_repo":null,"tracked_branch":null}`,
			check: func(t *testing.T, m workspaceModel) {
				if !m.VCSRepo.IsNull() {
					t.Errorf("vcs_repo = %v, want null after a disconnect", m.VCSRepo)
				}
			},
		},
		{
			name:  "keep the configured spelling",
			prior: func(m *workspaceModel) { m.VCSRepo = connectedVCS(t, "WebbPulse/infra", "main") },
			plan: func(m *workspaceModel) {
				m.VCSRepo = vcsObject(t, types.StringValue("webbpulse/INFRA"), types.StringValue("main"), types.StringValue("42"), types.StringValue("7"))
			},
			want:     `{"vcs_repo":"webbpulse/INFRA"}`,
			response: client.Workspace{VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("main"), VCSRepositoryID: strPtr("42"), VCSInstallationID: strPtr("7")},
			check: func(t *testing.T, m workspaceModel) {
				if !m.VCSRepo.Equal(connectedVCS(t, "webbpulse/INFRA", "main")) {
					t.Errorf("vcs_repo = %v, want the configured spelling kept", m.VCSRepo)
				}
			},
		},
		{
			name:     "set trigger patterns",
			plan:     func(m *workspaceModel) { m.TriggerPatterns = stringList(t, "/modules/**", "*.tf") },
			want:     `{"trigger_patterns":["/modules/**","*.tf"]}`,
			response: client.Workspace{TriggerPatterns: []string{"/modules/**", "*.tf"}},
		},
		{
			name:  "clear trigger patterns",
			prior: func(m *workspaceModel) { m.TriggerPatterns = stringList(t, "*.tf") },
			plan:  func(m *workspaceModel) { m.TriggerPatterns = stringList(t) },
			want:  `{"trigger_patterns":[]}`,
		},
		{
			name: "turn both flags off",
			plan: func(m *workspaceModel) {
				m.FileTriggersEnabled = types.BoolValue(false)
				m.SpeculativeEnabled = types.BoolValue(false)
			},
			want:     `{"speculative_plans":false,"file_triggers_enabled":false}`,
			response: client.Workspace{FileTriggersEnabled: boolPtr(false), SpeculativePlans: boolPtr(false)},
			check: func(t *testing.T, m workspaceModel) {
				if m.FileTriggersEnabled.ValueBool() || m.SpeculativeEnabled.ValueBool() {
					t.Error("flags did not follow the response")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prior := baseWorkspaceModel(t)
			if tc.prior != nil {
				tc.prior(&prior)
			}
			next := prior
			tc.plan(&next)

			response := tc.response
			response.WorkspaceID, response.Name, response.Engine, response.EngineVersion, response.CreatedAt = "ws-test", "example", "terraform", "1.9.8", "created"
			r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPatch {
					t.Errorf("unexpected %s", req.Method)
				}
				var body json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if string(body) != tc.want {
					t.Errorf("PATCH body = %s, want %s", body, tc.want)
				}
				_ = json.NewEncoder(w).Encode(response)
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
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestWorkspaceCreateSendsVCS checks a create carries the connection and the
// trigger settings, and state takes the branch the API resolved.
func TestWorkspaceCreateSendsVCS(t *testing.T) {
	ctx := context.Background()
	r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["vcs_repo"] != "WebbPulse/infra" || body["speculative_plans"] != false || body["file_triggers_enabled"] != true {
			t.Errorf("create body = %v", body)
		}
		if _, ok := body["tracked_branch"]; ok {
			t.Error("an unknown branch was sent")
		}
		if patterns, _ := body["trigger_patterns"].([]any); len(patterns) != 1 || patterns[0] != "*.tf" {
			t.Errorf("trigger_patterns = %v", body["trigger_patterns"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("main"), VCSRepositoryID: strPtr("42"), VCSInstallationID: strPtr("7"),
			TriggerPatterns: []string{"*.tf"}, SpeculativePlans: boolPtr(false), FileTriggersEnabled: boolPtr(true),
		})
	})}
	model := baseWorkspaceModel(t)
	model.WorkspaceID = types.StringUnknown()
	model.CreatedAt = types.StringUnknown()
	model.VCSRepo = plannedVCS(t, "WebbPulse/infra", types.StringUnknown())
	model.TriggerPatterns = stringList(t, "*.tf")
	model.SpeculativeEnabled = types.BoolValue(false)
	planned := resourceState(t, r, &model)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got workspaceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatal(diags)
	}
	if !got.VCSRepo.Equal(connectedVCS(t, "WebbPulse/infra", "main")) || got.SpeculativeEnabled.ValueBool() {
		t.Errorf("state did not follow the create response: %v", got.VCSRepo)
	}
}

// TestWorkspaceVCSRepoNotInstalled checks a 422 VCS_REPO_NOT_INSTALLED names
// the code and the fix and points at vcs_repo.identifier.
func TestWorkspaceVCSRepoNotInstalled(t *testing.T) {
	ctx := context.Background()
	r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":{"message":"The GitHub App is not installed on WebbPulse/private. Install it on the repository first.","error_code":"VCS_REPO_NOT_INSTALLED"}}`))
	})}
	model := baseWorkspaceModel(t)
	model.WorkspaceID = types.StringUnknown()
	model.VCSRepo = plannedVCS(t, "WebbPulse/private", types.StringUnknown())
	planned := resourceState(t, r, &model)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a repository the App cannot see raised no error")
	}
	d := resp.Diagnostics[0]
	for _, part := range []string{client.VCSRepoNotInstalledCode, "Install the App", "WebbPulse/private"} {
		if !strings.Contains(d.Detail(), part) {
			t.Errorf("diagnostic %q omits %q", d.Detail(), part)
		}
	}
	withPath, ok := d.(interface{ Path() path.Path })
	if !ok || !withPath.Path().Equal(path.Root("vcs_repo").AtName("identifier")) {
		t.Error("diagnostic does not point at vcs_repo.identifier")
	}
}

// TestSameRepositoryModifier checks computed VCS values carry over only while
// the block names the same repository.
func TestSameRepositoryModifier(t *testing.T) {
	ctx := context.Background()
	r := &workspaceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	for _, tc := range []struct {
		name  string
		prior types.Object
		plan  types.Object
		keep  bool
	}{
		{"same repository", connectedVCS(t, "WebbPulse/infra", "main"), plannedVCS(t, "WebbPulse/infra", types.StringUnknown()), true},
		{"same repository in another case", connectedVCS(t, "WebbPulse/infra", "main"), plannedVCS(t, "webbpulse/infra", types.StringUnknown()), true},
		{"another repository", connectedVCS(t, "WebbPulse/infra", "main"), plannedVCS(t, "WebbPulse/other", types.StringUnknown()), false},
		{"newly connected", types.ObjectNull(vcsRepoAttrTypes()), plannedVCS(t, "WebbPulse/infra", types.StringUnknown()), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := baseWorkspaceModel(t)
			prior.VCSRepo = tc.prior
			next := baseWorkspaceModel(t)
			next.VCSRepo = tc.plan
			state := resourceState(t, r, &prior)
			plan := resourceState(t, r, &next)
			stateValue := types.StringNull()
			if !tc.prior.IsNull() {
				stateValue = types.StringValue("main")
			}
			req := planmodifier.StringRequest{
				Path:        path.Root("vcs_repo").AtName("branch"),
				ConfigValue: types.StringNull(),
				PlanValue:   types.StringUnknown(),
				StateValue:  stateValue,
				Plan:        tfsdk.Plan(plan),
				State:       state,
			}
			resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
			sameRepositoryModifier{}.PlanModifyString(ctx, req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if kept := resp.PlanValue.Equal(types.StringValue("main")); kept != tc.keep {
				t.Errorf("plan value = %v, want kept %v", resp.PlanValue, tc.keep)
			}
		})
	}
}

// TestWorkspaceVCSSchema checks the block shape mirrors tfe_workspace.
func TestWorkspaceVCSSchema(t *testing.T) {
	var resp resource.SchemaResponse
	NewWorkspaceResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	block, ok := resp.Schema.Blocks["vcs_repo"].(schema.SingleNestedBlock)
	if !ok {
		t.Fatal("vcs_repo is not a single nested block")
	}
	branch := block.Attributes["branch"].(schema.StringAttribute)
	if !branch.Optional || !branch.Computed {
		t.Error("branch is not optional and computed")
	}
	for _, name := range []string{"repository_id", "installation_id"} {
		if attribute := block.Attributes[name].(schema.StringAttribute); !attribute.Computed || attribute.Optional {
			t.Errorf("%s is not computed only", name)
		}
	}
	for _, name := range []string{"working_directory", "trigger_patterns", "file_triggers_enabled", "speculative_enabled"} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("%s is missing", name)
		}
	}
}

// TestWorkspaceDataSourceReadsVCS checks the data source exposes the
// connection and trigger settings.
func TestWorkspaceDataSourceReadsVCS(t *testing.T) {
	ctx := context.Background()
	d := &workspaceDataSource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/workspaces/ws-test" {
			t.Errorf("unexpected path %s", req.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			VCSRepo: strPtr("WebbPulse/infra"), TrackedBranch: strPtr("main"), VCSRepositoryID: strPtr("42"), VCSInstallationID: strPtr("7"),
			TriggerPatterns: []string{"*.tf"},
		})
	})}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objectType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["workspace_id"] = tftypes.NewValue(tftypes.String, "ws-test")
	config := tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, values)}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var branch, identifier types.String
	var speculative types.Bool
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("vcs_repo").AtName("identifier"), &identifier)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("vcs_repo").AtName("branch"), &branch)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("speculative_enabled"), &speculative)...)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if identifier.ValueString() != "WebbPulse/infra" || branch.ValueString() != "main" || !speculative.ValueBool() {
		t.Errorf("data source read %v %v %v", identifier, branch, speculative)
	}
}
