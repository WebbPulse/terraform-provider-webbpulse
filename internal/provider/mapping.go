package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// applyWorkspace copies one API workspace onto a state model. The model's
// vcs_repo going in is the prior value, whose identifier spelling is kept when
// the API names the same repository.
func applyWorkspace(ctx context.Context, from *client.Workspace, into *workspaceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	setup, setupDiags := runRoleSetupObject(ctx, from.RunRoleSetup)
	diags.Append(setupDiags...)
	vcsRepo, vcsDiags := vcsRepoObject(ctx, from, into.VCSRepo)
	diags.Append(vcsDiags...)
	patterns, patternDiags := triggerPatternsList(ctx, from.TriggerPatterns)
	diags.Append(patternDiags...)
	arns, arnDiags := stringSetFromAPI(ctx, from.PlanAssumeRoleARNs)
	diags.Append(arnDiags...)
	secretARNs, secretDiags := stringSetFromAPI(ctx, from.PlanSecretARNs)
	diags.Append(secretDiags...)
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
	into.VCSRepo = vcsRepo
	into.TriggerPatterns = patterns
	into.FileTriggersEnabled = boolOrTrue(from.FileTriggersEnabled)
	into.SpeculativeEnabled = boolOrTrue(from.SpeculativePlans)
	into.PlanAssumeRoleARNs = arns
	into.PlanSecretARNs = secretARNs
	into.PlanRoleARN = optionalString(from.PlanRoleARN)
	into.AutoApply = types.BoolValue(from.AutoApply != nil && *from.AutoApply)
	into.ProjectID = types.StringValue(projectIDOrDefault(from.ProjectID))
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

// projectIDOrDefault reads an absent project id, from an API that predates
// projects, as the default project.
func projectIDOrDefault(projectID string) string {
	if projectID == "" {
		return client.DefaultProjectID
	}
	return projectID
}
