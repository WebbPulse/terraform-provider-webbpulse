package provider

import (
	"context"
	"fmt"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ action.Action              = (*runRoleCheckAction)(nil)
	_ action.ActionWithConfigure = (*runRoleCheckAction)(nil)
)

type runRoleCheckAction struct {
	client *client.Client
}

// NewRunRoleCheckAction returns the webbpulse_run_role_check action.
func NewRunRoleCheckAction() action.Action { return &runRoleCheckAction{} }

type runRoleCheckActionModel struct {
	WorkspaceID        types.String `tfsdk:"workspace_id"`
	FailIfNotConnected types.Bool   `tfsdk:"fail_if_not_connected"`
}

func (a *runRoleCheckAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_run_role_check"
}

func (a *runRoleCheckAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assumes one workspace's run role and reports whether it answered. An action " +
			"returns only diagnostics, so this one reports the outcome as a message and, when asked, fails " +
			"the apply. Use the `webbpulse_run_role_check` data source instead when a configuration needs " +
			"the account id as a value.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace whose run role is checked.",
			},
			"fail_if_not_connected": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Whether a role that does not answer fails the apply. Defaults to true. " +
					"Set it to false to report the outcome as a warning while the trust policy is still " +
					"being put in place.",
			},
		},
	}
}

func (a *runRoleCheckAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	configureClient(req.ProviderData, &a.client, &resp.Diagnostics)
}

func (a *runRoleCheckAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config runRoleCheckActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := config.WorkspaceID.ValueString()
	if resp.SendProgress != nil {
		resp.SendProgress(action.InvokeProgressEvent{
			Message: fmt.Sprintf("Assuming the run role for workspace %s", workspaceID),
		})
	}

	outcome, err := a.client.CheckRunRole(ctx, workspaceID)
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

	if outcome.Connected {
		accountID := ""
		if outcome.AccountID != nil {
			accountID = *outcome.AccountID
		}
		if resp.SendProgress != nil {
			resp.SendProgress(action.InvokeProgressEvent{
				Message: fmt.Sprintf("The run role answered from account %s", accountID),
			})
		}
		return
	}

	reason := "The role did not answer."
	if outcome.Error != nil {
		reason = *outcome.Error
	}
	summary := fmt.Sprintf("The run role for workspace %s did not answer", workspaceID)

	if config.FailIfNotConnected.IsNull() || config.FailIfNotConnected.ValueBool() {
		resp.Diagnostics.AddError(summary, reason)
		return
	}
	resp.Diagnostics.AddWarning(summary, reason)
}
