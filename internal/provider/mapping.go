package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// applyWorkspace copies one API workspace onto a state model.
func applyWorkspace(ctx context.Context, from *client.Workspace, into *workspaceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	setup, setupDiags := runRoleSetupObject(ctx, from.RunRoleSetup)
	diags.Append(setupDiags...)
	if diags.HasError() {
		return diags
	}

	into.WorkspaceID = types.StringValue(from.WorkspaceID)
	into.Name = types.StringValue(from.Name)
	into.Engine = types.StringValue(from.Engine)
	into.EngineVersion = types.StringValue(from.EngineVersion)
	into.RunRoleARN = optionalString(from.RunRoleARN)
	into.WorkingDirectory = types.StringValue(from.WorkingDirectory)
	into.Description = types.StringValue(from.Description)
	into.CreatedAt = types.StringValue(from.CreatedAt)
	into.UpdatedAt = optionalString(from.UpdatedAt)
	into.RunRoleSetup = setup
	into.RunRoleCheckedAt = optionalString(from.RunRoleCheckedAt)
	into.RunRoleAccountID = optionalString(from.RunRoleAccountID)
	return diags
}

// runRoleSetupObject renders one run role setup as its Terraform object value.
func runRoleSetupObject(ctx context.Context, setup client.RunRoleSetup) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	principalARNs, listDiags := types.ListValueFrom(ctx, types.StringType, setup.PrincipalARNs)
	diags.Append(listDiags...)
	if diags.HasError() {
		return types.ObjectNull(runRoleSetupAttrTypes()), diags
	}

	object, objectDiags := types.ObjectValue(runRoleSetupAttrTypes(), map[string]attr.Value{
		"principal_arn":  types.StringValue(setup.PrincipalARN),
		"principal_arns": principalARNs,
		"external_id":    types.StringValue(setup.ExternalID),
		"role_name":      types.StringValue(setup.RoleName),
	})
	diags.Append(objectDiags...)
	return object, diags
}

// optionalString turns a nullable API string into its Terraform value.
func optionalString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}
