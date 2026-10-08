package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*workspaceResource)(nil)
	_ resource.ResourceWithConfigure   = (*workspaceResource)(nil)
	_ resource.ResourceWithImportState = (*workspaceResource)(nil)
)

type workspaceResource struct {
	client *client.Client
}

// NewWorkspaceResource returns the webbpulse_workspace resource.
func NewWorkspaceResource() resource.Resource { return &workspaceResource{} }

type workspaceModel struct {
	WorkspaceID         types.String `tfsdk:"workspace_id"`
	Name                types.String `tfsdk:"name"`
	Engine              types.String `tfsdk:"engine"`
	EngineVersion       types.String `tfsdk:"engine_version"`
	RunRoleARN          types.String `tfsdk:"run_role_arn"`
	WorkingDirectory    types.String `tfsdk:"working_directory"`
	Description         types.String `tfsdk:"description"`
	CreatedAt           types.String `tfsdk:"created_at"`
	UpdatedAt           types.String `tfsdk:"updated_at"`
	RunRoleSetup        types.Object `tfsdk:"run_role_setup"`
	RunRoleCheckedAt    types.String `tfsdk:"run_role_checked_at"`
	RunRoleAccountID    types.String `tfsdk:"run_role_account_id"`
	ForceDelete         types.Bool   `tfsdk:"force_delete"`
	VCSRepo             types.Object `tfsdk:"vcs_repo"`
	TriggerPatterns     types.List   `tfsdk:"trigger_patterns"`
	FileTriggersEnabled types.Bool   `tfsdk:"file_triggers_enabled"`
	SpeculativeEnabled  types.Bool   `tfsdk:"speculative_enabled"`
	PlanAssumeRoleARNs  types.Set    `tfsdk:"plan_assume_role_arns"`
	PlanSecretARNs      types.Set    `tfsdk:"plan_secret_arns"`
	AutoApply           types.Bool   `tfsdk:"auto_apply"`
	ProjectID           types.String `tfsdk:"project_id"`
}

func runRoleSetupAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"principal_arn":  types.StringType,
		"principal_arns": types.ListType{ElemType: types.StringType},
		"external_id":    types.StringType,
		"role_name":      types.StringType,
	}
}

// Metadata sets the type name of the workspace resource.
func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

// Schema defines the schema of the workspace resource.
func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One workspace in the control plane. The name is unique across the environment " +
			"and is not editable, so changing it replaces the workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The workspace id, such as `ws-01J...`. Also the run role's external id.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The workspace name, unique across the environment. Not editable, so a " +
					"change replaces the workspace.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"engine": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(client.EngineTerraform),
				MarkdownDescription: "Which binary runs this workspace, `terraform` or `tofu`.",
			},
			"engine_version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The engine version this workspace runs, such as `1.9.8`.",
			},
			"run_role_arn": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The role the runner assumes for this workspace. Optional on create: the " +
					"role's trust policy names the workspace id as its external id, so the role cannot exist " +
					"until the workspace does. Build it from `run_role_setup`, then set this. Removing it " +
					"sends an explicit null and clears the role and its recorded check outcome.",
			},
			"working_directory": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
				MarkdownDescription: "The directory inside the configuration the engine runs in, relative " +
					"to the repository root when `vcs_repo` is set. Changed paths outside it do not trigger runs " +
					"unless `trigger_patterns` names them.",
			},
			"trigger_patterns": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				MarkdownDescription: "Glob patterns over repository paths, such as `/modules/**/*.tf`. An upload " +
					"from `vcs_repo` starts a run only when a changed path matches one. Empty, the default, " +
					"means every change under `working_directory`. Ignored while `file_triggers_enabled` is `false`.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(50),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(
						stringvalidator.LengthBetween(1, 255),
						stringvalidator.RegexMatches(regexp.MustCompile(`^\S(.*\S)?$`), "must not start or end with whitespace"),
					),
				},
			},
			"file_triggers_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether uploads are filtered by changed paths against `working_directory` " +
					"and `trigger_patterns`. `false` starts a run for every push to the tracked branch. Defaults " +
					"to `true`.",
			},
			"speculative_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether a pull request upload starts a plan only run. Sent to the API as " +
					"`speculative_plans`. Defaults to `true`.",
			},
			"auto_apply": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether a run whose plan has changes applies without a confirmation, like HCP " +
					"Terraform's auto-apply. Plan only and pull request runs never apply. Changing it needs a token " +
					"with `admin`. Defaults to `false`.",
			},
			"project_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.DefaultProjectID),
				MarkdownDescription: "The project the workspace belongs to, such as `webbpulse_project.example.id`. " +
					"Unset, or `prj-default`, means the default project, which every workspace not moved " +
					"elsewhere sits in. Changing it moves the workspace in place; its state and runs stay put. " +
					"A project that does not exist is refused with `PROJECT_NOT_FOUND`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(projectIDPattern), "must be a project id such as prj-default"),
				},
			},
			"plan_assume_role_arns": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "Exact IAM role ARNs a plan session may assume beside its read only access, " +
					"such as a Route 53 reader role in another account. At most 10, each up to 160 characters, " +
					"with no wildcards. An apply is not limited by this list. Removing it, or setting it to " +
					"`[]`, sends an explicit null and clears the list.",
				Validators: planAssumeRoleARNsValidators(),
			},
			"plan_secret_arns": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "Secrets Manager ARN patterns whose values a plan session may read, such as " +
					"`arn:aws:secretsmanager:*:111122223333:secret:app-*`. At most 10, each up to 200 characters " +
					"and pinned to one account; the region and the name may hold `*` or `?` wildcards. When the " +
					"list is empty, a confirmable plan may read any secret and a speculative plan, such as a pull " +
					"request plan, may read none. An apply is not limited by this list. Setting it needs `admin` " +
					"or the factory grant, and a recent sign in for a person. Removing it, or setting it to `[]`, " +
					"sends an explicit null and clears the list.",
				Validators: planSecretARNsValidators(),
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "A description for this workspace.",
			},
			"force_delete": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying this resource deletes the workspace even when its state " +
					"still tracks resources, leaving them unmanaged. Defaults to `false`, so a workspace that " +
					"still manages resources is refused with `WORKSPACE_MANAGES_RESOURCES` until they are " +
					"destroyed. Set it and apply before the destroy, because a destroy reads it from state. It " +
					"never skips the `WORKSPACE_HAS_ACTIVE_RUN` refusal while a run is active.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the workspace was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the workspace was last edited.",
			},
			"run_role_checked_at": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "When the run that proved the current run role assumed it, or null " +
					"when none has. The web UI's run role check stamps this; the data source does not.",
			},
			"run_role_account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account the run role resolved to on its last stamped check.",
			},
			"run_role_setup": schema.SingleNestedAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Everything needed to build this workspace's run role. The values are " +
					"derived from the workspace id, so they are known only once the workspace exists.",
				Attributes: map[string]schema.Attribute{
					"principal_arn": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "The first runner task role the trust policy has to name.",
					},
					"principal_arns": schema.ListAttribute{
						Computed:    true,
						ElementType: types.StringType,
						MarkdownDescription: "Every runner task role, one per phase. A trust policy naming " +
							"only the plan role leaves the apply phase unable to assume, so all of them belong in it.",
					},
					"external_id": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "The workspace id, which the runner sends as `sts:ExternalId`.",
					},
					"role_name": schema.StringAttribute{
						Computed: true,
						MarkdownDescription: "The name the role has to carry to fall inside the runner's " +
							"AssumeRole grant.",
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": schema.SingleNestedBlock{
				MarkdownDescription: "Connects the workspace to a GitHub repository through the environment's " +
					"GitHub App, so pushes to `branch` start runs and pull requests start plan only runs. " +
					"Removing the block disconnects the repository.",
				Attributes: map[string]schema.Attribute{
					"identifier": schema.StringAttribute{
						Optional: true,
						MarkdownDescription: "The repository as `owner/name`. Required when the block is set. " +
							"The App has to be installed on it, or the apply fails with `VCS_REPO_NOT_INSTALLED`.",
						Validators: []validator.String{
							stringvalidator.LengthAtMost(140),
							stringvalidator.RegexMatches(regexp.MustCompile(vcsRepoPattern), "must be a GitHub owner/name"),
						},
					},
					"branch": schema.StringAttribute{
						Optional: true,
						Computed: true,
						MarkdownDescription: "The branch whose pushes start runs. Defaults to the repository's " +
							"default branch, which the API resolves when the repository is connected.",
						Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
						PlanModifiers: []planmodifier.String{sameRepositoryModifier{}},
					},
					"repository_id": schema.StringAttribute{
						Computed: true,
						MarkdownDescription: "GitHub's id for the repository, which keeps the connection " +
							"through a rename. Null until resolved.",
						PlanModifiers: []planmodifier.String{sameRepositoryModifier{}},
					},
					"installation_id": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "The GitHub App installation that covered the repository when it was connected.",
						PlanModifiers:       []planmodifier.String{sameRepositoryModifier{}},
					},
				},
				Validators: []validator.Object{objectvalidator.AlsoRequires(path.MatchRelative().AtName("identifier"))},
			},
		},
	}
}

// Configure stores the shared API client on the workspace resource.
func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// Create creates the workspace resource through the API and records the result in state.
func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.WorkspaceCreate{
		Name:             plan.Name.ValueString(),
		Engine:           plan.Engine.ValueString(),
		EngineVersion:    plan.EngineVersion.ValueString(),
		WorkingDirectory: plan.WorkingDirectory.ValueString(),
		Description:      plan.Description.ValueString(),
	}
	if !plan.RunRoleARN.IsNull() && !plan.RunRoleARN.IsUnknown() {
		arn := plan.RunRoleARN.ValueString()
		body.RunRoleARN = &arn
	}
	repo, branch, vcsDiags := vcsPatch(ctx, plan.VCSRepo, types.ObjectNull(vcsRepoAttrTypes()))
	resp.Diagnostics.Append(vcsDiags...)
	patterns, patternDiags := listStrings(ctx, plan.TriggerPatterns)
	resp.Diagnostics.Append(patternDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if repo != nil {
		body.VCSRepo = *repo
	}
	if branch != nil {
		body.TrackedBranch = *branch
	}
	body.TriggerPatterns = patterns
	body.FileTriggersEnabled = plan.FileTriggersEnabled.ValueBoolPointer()
	body.SpeculativePlans = plan.SpeculativeEnabled.ValueBoolPointer()
	if plan.AutoApply.ValueBool() {
		body.AutoApply = plan.AutoApply.ValueBoolPointer()
	}
	arns, arnDiags := setStrings(ctx, plan.PlanAssumeRoleARNs)
	resp.Diagnostics.Append(arnDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	body.PlanAssumeRoleARNs = arns
	secretARNs, secretDiags := setStrings(ctx, plan.PlanSecretARNs)
	resp.Diagnostics.Append(secretDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	body.PlanSecretARNs = secretARNs
	if projectID := plan.ProjectID.ValueString(); projectID != client.DefaultProjectID {
		body.ProjectID = projectID
	}

	created, err := r.client.CreateWorkspace(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(workspaceWriteDiagnostic("Cannot create the workspace", err))
		return
	}

	state := workspaceModel{ForceDelete: plan.ForceDelete, VCSRepo: plan.VCSRepo}
	resp.Diagnostics.Append(applyWorkspace(ctx, created, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Read refreshes the workspace, dropping it from state on a 404.
func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetWorkspace(ctx, state.WorkspaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the workspace", err))
		return
	}

	resp.Diagnostics.Append(applyWorkspace(ctx, found, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.ForceDelete.IsNull() {
		state.ForceDelete = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update sends a merge patch of the changed attributes and records the response in state.
func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.WorkspaceUpdate{
		Engine:              changedString(plan.Engine, state.Engine),
		EngineVersion:       changedString(plan.EngineVersion, state.EngineVersion),
		RunRoleARN:          clearablePatch(plan.RunRoleARN, state.RunRoleARN),
		WorkingDirectory:    clearablePatch(plan.WorkingDirectory, state.WorkingDirectory),
		Description:         clearablePatch(plan.Description, state.Description),
		FileTriggersEnabled: changedBool(plan.FileTriggersEnabled, state.FileTriggersEnabled),
		SpeculativePlans:    changedBool(plan.SpeculativeEnabled, state.SpeculativeEnabled),
		AutoApply:           changedBool(plan.AutoApply, state.AutoApply),
		ProjectID:           changedString(plan.ProjectID, state.ProjectID),
	}
	var vcsDiags diag.Diagnostics
	body.VCSRepo, body.TrackedBranch, vcsDiags = vcsPatch(ctx, plan.VCSRepo, state.VCSRepo)
	resp.Diagnostics.Append(vcsDiags...)
	if !plan.TriggerPatterns.IsUnknown() && !plan.TriggerPatterns.Equal(state.TriggerPatterns) {
		patterns, patternDiags := listStrings(ctx, plan.TriggerPatterns)
		resp.Diagnostics.Append(patternDiags...)
		if patterns == nil {
			patterns = []string{}
		}
		body.TriggerPatterns = &patterns
	}
	var arnDiags diag.Diagnostics
	body.PlanAssumeRoleARNs, arnDiags = stringSetPatch(ctx, plan.PlanAssumeRoleARNs, state.PlanAssumeRoleARNs)
	resp.Diagnostics.Append(arnDiags...)
	var secretDiags diag.Diagnostics
	body.PlanSecretARNs, secretDiags = stringSetPatch(ctx, plan.PlanSecretARNs, state.PlanSecretARNs)
	resp.Diagnostics.Append(secretDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateWorkspace(ctx, state.WorkspaceID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.Append(workspaceWriteDiagnostic("Cannot update the workspace", err))
		return
	}

	next := workspaceModel{ForceDelete: plan.ForceDelete, VCSRepo: plan.VCSRepo}
	resp.Diagnostics.Append(applyWorkspace(ctx, updated, &next)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Delete deletes the workspace resource, treating a 404 as already gone.
func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspace(ctx, state.WorkspaceID.ValueString(), state.ForceDelete.ValueBool())
	if err == nil || client.IsNotFound(err) {
		return
	}
	resp.Diagnostics.Append(deleteWorkspaceDiagnostic(err))
}

// deleteWorkspaceDiagnostic turns a refused workspace delete into a diagnostic
// that names the API's error code and what to do about it.
func deleteWorkspaceDiagnostic(err error) diag.Diagnostic {
	switch client.ErrorCode(err) {
	case client.WorkspaceManagesResourcesCode:
		return diag.NewErrorDiagnostic(
			"The workspace still manages resources",
			client.WorkspaceManagesResourcesCode+": the workspace state still tracks resources, so deleting it "+
				"would leave them running and unmanaged. Destroy them first with a destroy run on the "+
				"workspace, or set force_delete = true on the webbpulse_workspace resource and apply that "+
				"change before destroying to delete it anyway. "+err.Error(),
		)
	case client.WorkspaceHasActiveRunCode:
		return diag.NewErrorDiagnostic(
			"The workspace has an active run",
			client.WorkspaceHasActiveRunCode+": a run on the workspace has not finished. Wait for it to "+
				"finish or cancel it, then destroy again. force_delete does not skip this check. "+err.Error(),
		)
	}
	return apiDiagnostic("Cannot delete the workspace", err)
}

// ImportState imports a workspace by its id.
func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("workspace_id"), req, resp)
}

// changedString is the merge patch value for a field that cannot be cleared:
// nil, so the key is omitted, when the plan matches the state.
func changedString(plan, state types.String) *string {
	if plan.Equal(state) {
		return nil
	}
	return plan.ValueStringPointer()
}

// clearablePatch is the merge patch value for a clearable field. It is nil, so
// the key is omitted, when the plan matches the state, and a pointer to a nil
// string, which encodes as an explicit JSON null, when the attribute was removed
// from configuration. A removed working_directory or description plans as its
// empty default, so an empty string clears too.
func clearablePatch(plan, state types.String) **string {
	if plan.Equal(state) {
		return nil
	}
	value := plan.ValueStringPointer()
	if value != nil && *value == "" {
		value = nil
	}
	return &value
}
