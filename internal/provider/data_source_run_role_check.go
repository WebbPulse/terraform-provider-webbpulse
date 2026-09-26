package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*runRoleCheckDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*runRoleCheckDataSource)(nil)
)

type runRoleCheckDataSource struct {
	client *client.Client
}

// NewRunRoleCheckDataSource returns the webbpulse_run_role_check data source.
func NewRunRoleCheckDataSource() datasource.DataSource { return &runRoleCheckDataSource{} }

type runRoleCheckModel struct {
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Connected   types.Bool   `tfsdk:"connected"`
	AccountID   types.String `tfsdk:"account_id"`
	Error       types.String `tfsdk:"error"`
}

// Metadata sets the type name of the run role check data source.
func (d *runRoleCheckDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_run_role_check"
}

// Schema defines the schema of the run role check data source.
func (d *runRoleCheckDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assumes one workspace's run role and reports whether it answered. It calls " +
			"`GET /workspaces/{id}/run-role/check`, which probes the role and records nothing, so plans and " +
			"refreshes never change the workspace. A role that cannot be assumed is reported as " +
			"`connected = false` with the reason in `error` rather than failing the plan. A workspace with " +
			"no run role at all is an error.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace whose run role is checked.",
			},
			"connected": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the role answered an AssumeRole.",
			},
			"account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account the role resolved to, or null when it did not answer.",
			},
			"error": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Why the role did not answer, as one sentence, or null on success. " +
					"A configured role that does not answer is not an error here: the trust policy may " +
					"simply not be in place yet, so the plan succeeds and shows the reason in this " +
					"attribute.",
			},
		},
	}
}

// Configure stores the shared API client on the run role check data source.
func (d *runRoleCheckDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read probes the run role through the read-only GET route and records the outcome as data.
func (d *runRoleCheckDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config runRoleCheckModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	outcome, err := d.client.ReadRunRoleCheck(ctx, config.WorkspaceID.ValueString())
	if err != nil {
		if client.ErrorCode(err) == client.RunRoleMissingCode {
			resp.Diagnostics.AddError(
				"The workspace has no run role",
				"Set run_role_arn on the workspace before checking it. "+err.Error(),
			)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot check the run role", err))
		return
	}

	config.Connected = types.BoolValue(outcome.Connected)
	config.AccountID = optionalString(outcome.AccountID)
	config.Error = optionalString(outcome.Error)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
