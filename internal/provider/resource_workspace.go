package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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
	WorkspaceID      types.String `tfsdk:"workspace_id"`
	Name             types.String `tfsdk:"name"`
	Engine           types.String `tfsdk:"engine"`
	EngineVersion    types.String `tfsdk:"engine_version"`
	RunRoleARN       types.String `tfsdk:"run_role_arn"`
	WorkingDirectory types.String `tfsdk:"working_directory"`
	Description      types.String `tfsdk:"description"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
	RunRoleSetup     types.Object `tfsdk:"run_role_setup"`
	RunRoleCheckedAt types.String `tfsdk:"run_role_checked_at"`
	RunRoleAccountID types.String `tfsdk:"run_role_account_id"`
}

func runRoleSetupAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"principal_arn":  types.StringType,
		"principal_arns": types.ListType{ElemType: types.StringType},
		"external_id":    types.StringType,
		"role_name":      types.StringType,
	}
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

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
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "The directory inside the configuration the engine runs in.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "A description for this workspace.",
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
				MarkdownDescription: "When the run role last answered an AssumeRole, or null when it never " +
					"has. A run role check writes this.",
			},
			"run_role_account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account the run role resolved to on its last successful check.",
			},
			"run_role_setup": schema.SingleNestedAttribute{
				Computed: true,
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
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

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

	created, err := r.client.CreateWorkspace(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot create the workspace", err))
		return
	}

	state := workspaceModel{}
	resp.Diagnostics.Append(applyWorkspace(ctx, created, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

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
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

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

	engine := plan.Engine.ValueString()
	engineVersion := plan.EngineVersion.ValueString()
	workingDirectory := plan.WorkingDirectory.ValueStringPointer()
	description := plan.Description.ValueStringPointer()
	body := client.WorkspaceUpdate{
		Engine:           &engine,
		EngineVersion:    &engineVersion,
		WorkingDirectory: &workingDirectory,
		Description:      &description,
	}
	if plan.RunRoleARN.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("run_role_arn"),
			"The run role is unknown",
			"The run role must be known before updating the workspace.",
		)
		return
	}
	if !plan.RunRoleARN.Equal(state.RunRoleARN) {
		arn := plan.RunRoleARN.ValueStringPointer()
		body.RunRoleARN = &arn
	}

	updated, err := r.client.UpdateWorkspace(ctx, state.WorkspaceID.ValueString(), body)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot update the workspace", err))
		return
	}

	next := workspaceModel{}
	resp.Diagnostics.Append(applyWorkspace(ctx, updated, &next)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteWorkspace(ctx, state.WorkspaceID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot delete the workspace", err))
	}
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("workspace_id"), req, resp)
}
