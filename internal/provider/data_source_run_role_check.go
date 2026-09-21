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

func (d *runRoleCheckDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_run_role_check"
}

func (d *runRoleCheckDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assumes one workspace's run role and reports whether it answered. This is a " +
			"data source rather than an action because it returns values a configuration reads: an action's " +
			"`Invoke` hands back only diagnostics and progress messages, so `connected`, `account_id` and " +
			"`error` could not reach state through one. The read is pure: it calls the API's read-only " +
			"route, which performs the AssumeRole and returns the outcome without recording it on the " +
			"workspace, so repeated plans and refreshes change nothing. A role that cannot be assumed is " +
			"reported as `connected = false` with the reason in `error` rather than failing the plan. Use " +
			"the `webbpulse_run_role_check` action instead when a failure should stop an apply.",
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

func (d *runRoleCheckDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

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
