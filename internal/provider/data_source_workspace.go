package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*workspaceDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*workspaceDataSource)(nil)
)

type workspaceDataSource struct {
	client *client.Client
}

// NewWorkspaceDataSource returns the webbpulse_workspace data source.
func NewWorkspaceDataSource() datasource.DataSource { return &workspaceDataSource{} }

// workspaceDataModel is the workspace resource model without the resource
// only force_delete, which the data source schema does not carry.
type workspaceDataModel struct {
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
	VCSRepo             types.Object `tfsdk:"vcs_repo"`
	TriggerPatterns     types.List   `tfsdk:"trigger_patterns"`
	FileTriggersEnabled types.Bool   `tfsdk:"file_triggers_enabled"`
	SpeculativeEnabled  types.Bool   `tfsdk:"speculative_enabled"`
	AutoApply           types.Bool   `tfsdk:"auto_apply"`
	PlanAssumeRoleARNs  types.Set    `tfsdk:"plan_assume_role_arns"`
	PlanSecretARNs      types.Set    `tfsdk:"plan_secret_arns"`
	ProjectID           types.String `tfsdk:"project_id"`
}

// workspaceDataFrom narrows a resource model to the data source model.
func workspaceDataFrom(m workspaceModel) *workspaceDataModel {
	return &workspaceDataModel{
		WorkspaceID:         m.WorkspaceID,
		Name:                m.Name,
		Engine:              m.Engine,
		EngineVersion:       m.EngineVersion,
		RunRoleARN:          m.RunRoleARN,
		WorkingDirectory:    m.WorkingDirectory,
		Description:         m.Description,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
		RunRoleSetup:        m.RunRoleSetup,
		RunRoleCheckedAt:    m.RunRoleCheckedAt,
		RunRoleAccountID:    m.RunRoleAccountID,
		VCSRepo:             m.VCSRepo,
		TriggerPatterns:     m.TriggerPatterns,
		FileTriggersEnabled: m.FileTriggersEnabled,
		SpeculativeEnabled:  m.SpeculativeEnabled,
		AutoApply:           m.AutoApply,
		PlanAssumeRoleARNs:  m.PlanAssumeRoleARNs,
		PlanSecretARNs:      m.PlanSecretARNs,
		ProjectID:           m.ProjectID,
	}
}

// Metadata sets the type name of the workspace data source.
func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

// Schema defines the schema of the workspace data source.
func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One workspace, looked up by id or by name. Exactly one of `workspace_id` and " +
			"`name` is set. The API has no name lookup route, so a lookup by name lists every workspace and " +
			"filters in the provider.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The workspace id to look up.",
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The workspace name to look up.",
			},
			"engine":            schema.StringAttribute{Computed: true, MarkdownDescription: "Which binary runs this workspace."},
			"engine_version":    schema.StringAttribute{Computed: true, MarkdownDescription: "The engine version this workspace runs."},
			"run_role_arn":      schema.StringAttribute{Computed: true, MarkdownDescription: "The role the runner assumes for this workspace."},
			"working_directory": schema.StringAttribute{Computed: true, MarkdownDescription: "The directory the engine runs in."},
			"description":       schema.StringAttribute{Computed: true, MarkdownDescription: "The workspace description."},
			"created_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "When the workspace was created."},
			"updated_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "When the workspace was last edited."},
			"run_role_checked_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the run role last answered an AssumeRole.",
			},
			"run_role_account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account the run role resolved to on its last successful check.",
			},
			"trigger_patterns": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Glob patterns over repository paths that decide whether an upload starts a run.",
			},
			"file_triggers_enabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether uploads are filtered by changed paths.",
			},
			"speculative_enabled": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether a pull request upload starts a plan only run.",
			},
			"auto_apply": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether a run whose plan has changes applies without a confirmation.",
			},
			"project_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The project the workspace belongs to, `prj-default` for the default project.",
			},
			"plan_assume_role_arns": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Exact IAM role ARNs a plan session may assume beside its read only access.",
			},
			"plan_secret_arns": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Secrets Manager ARN patterns whose values a plan session may read.",
			},
			"vcs_repo": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The connected GitHub repository, or null when none is connected.",
				Attributes: map[string]schema.Attribute{
					"identifier":      schema.StringAttribute{Computed: true, MarkdownDescription: "The repository as `owner/name`."},
					"branch":          schema.StringAttribute{Computed: true, MarkdownDescription: "The branch whose pushes start runs."},
					"repository_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "GitHub's id for the repository."},
					"installation_id": schema.StringAttribute{Computed: true, MarkdownDescription: "The GitHub App installation that covered the repository."},
				},
			},
			"run_role_setup": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Everything needed to build this workspace's run role.",
				Attributes: map[string]schema.Attribute{
					"principal_arn":  schema.StringAttribute{Computed: true, MarkdownDescription: "The first runner task role the trust policy has to name."},
					"principal_arns": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Every runner task role, one per phase."},
					"external_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "The workspace id, sent as `sts:ExternalId`."},
					"role_name":      schema.StringAttribute{Computed: true, MarkdownDescription: "The name the role has to carry."},
				},
			},
		},
	}
}

// Configure stores the shared API client on the workspace data source.
func (d *workspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read looks one workspace up by id or by name.
func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config workspaceDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !config.WorkspaceID.IsNull() && config.WorkspaceID.ValueString() != ""
	hasName := !config.Name.IsNull() && config.Name.ValueString() != ""
	if hasID == hasName {
		resp.Diagnostics.AddError(
			"Set exactly one of workspace_id and name",
			"Look a workspace up either by its id or by its name, not by both and not by neither.",
		)
		return
	}

	var (
		found *client.Workspace
		err   error
	)
	if hasID {
		found, err = d.client.GetWorkspace(ctx, config.WorkspaceID.ValueString())
	} else {
		found, err = d.client.GetWorkspaceByName(ctx, config.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the workspace", err))
		return
	}

	state := workspaceModel{}
	resp.Diagnostics.Append(applyWorkspace(ctx, found, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, workspaceDataFrom(state))...)
}
