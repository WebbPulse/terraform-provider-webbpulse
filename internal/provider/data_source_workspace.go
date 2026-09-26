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
	var config workspaceModel
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
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
