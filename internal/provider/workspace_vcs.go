package provider

import (
	"context"
	"strings"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// vcsRepoPattern is the GitHub owner/name form the API accepts for vcs_repo.
const vcsRepoPattern = `^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`

// vcsRepoFields is the Go view of one vcs_repo object.
type vcsRepoFields struct {
	Identifier     types.String `tfsdk:"identifier"`
	Branch         types.String `tfsdk:"branch"`
	RepositoryID   types.String `tfsdk:"repository_id"`
	InstallationID types.String `tfsdk:"installation_id"`
}

func vcsRepoAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"identifier":      types.StringType,
		"branch":          types.StringType,
		"repository_id":   types.StringType,
		"installation_id": types.StringType,
	}
}

// readVCSRepo decodes a vcs_repo object, reporting ok false when it is null or unknown.
func readVCSRepo(ctx context.Context, object types.Object) (vcsRepoFields, bool, diag.Diagnostics) {
	var fields vcsRepoFields
	if object.IsNull() || object.IsUnknown() {
		return fields, false, nil
	}
	diags := object.As(ctx, &fields, basetypes.ObjectAsOptions{})
	return fields, !diags.HasError(), diags
}

// vcsRepoObject renders the API's connection as a vcs_repo object. The prior
// identifier is kept when it names the same repository in another case, since
// the API stores GitHub's canonical spelling.
func vcsRepoObject(ctx context.Context, from *client.Workspace, prior types.Object) (types.Object, diag.Diagnostics) {
	if from.VCSRepo == nil || *from.VCSRepo == "" {
		return types.ObjectNull(vcsRepoAttrTypes()), nil
	}
	identifier := *from.VCSRepo
	previous, ok, diags := readVCSRepo(ctx, prior)
	if diags.HasError() {
		return types.ObjectNull(vcsRepoAttrTypes()), diags
	}
	if ok && !previous.Identifier.IsUnknown() && strings.EqualFold(previous.Identifier.ValueString(), identifier) {
		identifier = previous.Identifier.ValueString()
	}
	object, objectDiags := types.ObjectValueFrom(ctx, vcsRepoAttrTypes(), vcsRepoFields{
		Identifier:     types.StringValue(identifier),
		Branch:         optionalString(from.TrackedBranch),
		RepositoryID:   optionalString(from.VCSRepositoryID),
		InstallationID: optionalString(from.VCSInstallationID),
	})
	diags.Append(objectDiags...)
	return object, diags
}

// vcsPatch is the merge patch pair for vcs_repo and tracked_branch. Removing the
// block sends an explicit null for both, which disconnects the repository. A
// branch is sent whenever it is known and changed, and always alongside a new
// repository, because the API fills the default branch when a new repository
// arrives without one.
func vcsPatch(ctx context.Context, plan, state types.Object) (repo, branch **string, diags diag.Diagnostics) {
	planned, hasPlan, planDiags := readVCSRepo(ctx, plan)
	diags.Append(planDiags...)
	prior, hasState, stateDiags := readVCSRepo(ctx, state)
	diags.Append(stateDiags...)
	if diags.HasError() {
		return nil, nil, diags
	}

	if !hasPlan {
		if !hasState {
			return nil, nil, diags
		}
		var cleared *string
		return &cleared, &cleared, diags
	}

	if !hasState || !planned.Identifier.Equal(prior.Identifier) {
		value := planned.Identifier.ValueStringPointer()
		repo = &value
	}
	newRepository := !hasState || !strings.EqualFold(planned.Identifier.ValueString(), prior.Identifier.ValueString())
	branchKnown := !planned.Branch.IsUnknown() && !planned.Branch.IsNull()
	if branchKnown && (newRepository || !planned.Branch.Equal(prior.Branch)) {
		value := planned.Branch.ValueStringPointer()
		branch = &value
	}
	return repo, branch, diags
}

// triggerPatternsList renders the API's patterns as a list, empty rather than null.
func triggerPatternsList(ctx context.Context, patterns []string) (types.List, diag.Diagnostics) {
	if patterns == nil {
		patterns = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, patterns)
}

// listStrings reads a known list of strings, or nil when it is null or unknown.
func listStrings(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	out := []string{}
	diags := list.ElementsAs(ctx, &out, false)
	return out, diags
}

// boolOrTrue reads a flag the API defaults to true.
func boolOrTrue(value *bool) types.Bool {
	if value == nil {
		return types.BoolValue(true)
	}
	return types.BoolValue(*value)
}

// changedBool is the merge patch value for a flag: nil when the plan matches the state.
func changedBool(plan, state types.Bool) *bool {
	if plan.Equal(state) || plan.IsUnknown() || plan.IsNull() {
		return nil
	}
	return plan.ValueBoolPointer()
}

// workspaceWriteDiagnostic explains a refused create or update, pointing a
// repository the GitHub App cannot see at vcs_repo.identifier.
func workspaceWriteDiagnostic(summary string, err error) diag.Diagnostic {
	identifier := path.Root("vcs_repo").AtName("identifier")
	switch client.ErrorCode(err) {
	case client.VCSRepoNotInstalledCode:
		return diag.NewAttributeErrorDiagnostic(
			identifier,
			"The GitHub App cannot see this repository",
			client.VCSRepoNotInstalledCode+": the environment's GitHub App is not installed on the repository "+
				"named in vcs_repo.identifier, or its installation does not grant access to it. Install the App "+
				"on the repository, or add the repository to the installation, check the owner/name spelling, "+
				"then apply again. "+err.Error(),
		)
	case client.GitHubUnavailableCode:
		return diag.NewAttributeErrorDiagnostic(
			identifier,
			"GitHub could not resolve the repository",
			client.GitHubUnavailableCode+": the API could not reach GitHub to resolve vcs_repo.identifier. "+
				"This is usually transient, so apply again shortly. "+err.Error(),
		)
	case client.ProjectNotFoundCode:
		return diag.NewAttributeErrorDiagnostic(
			path.Root("project_id"),
			"The project does not exist",
			client.ProjectNotFoundCode+": project_id names no project in this environment. Create it with "+
				"a webbpulse_project resource and reference its id, or leave project_id unset for the "+
				"default project. "+err.Error(),
		)
	}
	return apiDiagnostic(summary, err)
}

// sameRepositoryModifier keeps a vcs_repo attribute's prior value while the
// block still names the same repository, and leaves it unknown when the block
// is new or names another repository, which is when the API resolves it again.
type sameRepositoryModifier struct{}

// Description describes the modifier in plain text.
func (sameRepositoryModifier) Description(context.Context) string {
	return "Keeps the prior value while vcs_repo.identifier names the same repository."
}

// MarkdownDescription describes the modifier in Markdown.
func (m sameRepositoryModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// PlanModifyString copies the prior value into the plan when the repository is unchanged.
func (sameRepositoryModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() || !req.PlanValue.IsUnknown() || req.State.Raw.IsNull() {
		return
	}
	identifier := path.Root("vcs_repo").AtName("identifier")
	var planned, prior types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, identifier, &planned)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, identifier, &prior)...)
	if resp.Diagnostics.HasError() || planned.IsUnknown() || planned.IsNull() || prior.IsNull() {
		return
	}
	if strings.EqualFold(planned.ValueString(), prior.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}
