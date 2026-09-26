package provider

import (
	"context"
	"fmt"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	WorkspaceID        types.String `tfsdk:"workspace_id"`
	FailIfNotConnected types.Bool   `tfsdk:"fail_if_not_connected"`
	Status             types.String `tfsdk:"status"`
	Connected          types.Bool   `tfsdk:"connected"`
	AccountID          types.String `tfsdk:"account_id"`
	Error              types.String `tfsdk:"error"`
	RunID              types.String `tfsdk:"run_id"`
	CheckedAt          types.String `tfsdk:"checked_at"`
}

// Metadata sets the type name of the run role check data source.
func (d *runRoleCheckDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_run_role_check"
}

// Schema defines the schema of the run role check data source.
func (d *runRoleCheckDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reports whether the runner has assumed one workspace's run role. It calls " +
			"`GET /workspaces/{id}/run-role/check`, which reads the runner's own record and records nothing, " +
			"so plans and refreshes never change the workspace. The API never calls STS: the answer is the " +
			"outcome of the runner's AssumeRole in the newest run made with the current role, and a role no " +
			"run has tried yet is `unverified`. A plan-only run is the check. A workspace with no run role " +
			"at all is an error.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace whose run role is checked.",
			},
			"fail_if_not_connected": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Whether any `status` other than `connected` fails the read. Defaults to " +
					"`false`, which reports the outcome as data instead. An `unverified` role fails too, " +
					"because no run has proven it yet.",
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "`connected` when the runner assumed the current role, `failed` when it " +
					"tried and was refused, or `unverified` when no plan has run on this role yet.",
			},
			"connected": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether `status` is `connected`.",
			},
			"account_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account the role resolved to, or null unless `status` is `connected`.",
			},
			"error": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Why the runner could not assume the role, as one sentence, or null. " +
					"A role that does not answer is not an error here unless `fail_if_not_connected` is set: " +
					"the trust policy may simply not be in place yet, so the plan succeeds and shows the " +
					"reason in this attribute.",
			},
			"run_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The run whose AssumeRole this outcome comes from, or null when `unverified`.",
			},
			"checked_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When that run tried the role, or null when `unverified`.",
			},
		},
	}
}

// Configure stores the shared API client on the run role check data source.
func (d *runRoleCheckDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read fetches the runner's record through the read-only GET route and records the outcome as data.
func (d *runRoleCheckDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config runRoleCheckModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := config.WorkspaceID.ValueString()
	outcome, err := d.client.ReadRunRoleCheck(ctx, workspaceID)
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

	if config.FailIfNotConnected.ValueBool() && outcome.Status != client.RunRoleStatusConnected {
		resp.Diagnostics.Append(notConnectedDiagnostic(workspaceID, outcome))
		return
	}

	config.Status = types.StringValue(outcome.Status)
	config.Connected = types.BoolValue(outcome.Connected)
	config.AccountID = optionalString(outcome.AccountID)
	config.Error = optionalString(outcome.Error)
	config.RunID = optionalString(outcome.RunID)
	config.CheckedAt = optionalString(outcome.CheckedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// notConnectedDiagnostic explains a run role check whose status is not connected.
func notConnectedDiagnostic(workspaceID string, outcome *client.RunRoleCheck) diag.Diagnostic {
	if outcome.Status == client.RunRoleStatusUnverified {
		return diag.NewErrorDiagnostic(
			fmt.Sprintf("The run role for workspace %s is unverified", workspaceID),
			"Status unverified means no plan has run on this role yet. The API does not call STS, so a "+
				"plan-only run on the workspace is the check: queue one, then read this data source again.",
		)
	}
	reason := "The runner could not assume the role."
	if outcome.Error != nil {
		reason = *outcome.Error
	}
	if outcome.RunID != nil {
		reason += fmt.Sprintf(" Reported by run %s.", *outcome.RunID)
	}
	return diag.NewErrorDiagnostic(
		fmt.Sprintf("The run role for workspace %s is not connected (status %s)", workspaceID, outcome.Status),
		reason,
	)
}
