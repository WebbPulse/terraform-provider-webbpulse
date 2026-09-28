package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// StepUpRequiredCode is the stable code a 401 carries when a user session's
// sign in is too old for a registry connect or delete.
const StepUpRequiredCode = "STEP_UP_REQUIRED"

const registryRepoPattern = `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`

type registryVCSRepoModel struct {
	Identifier types.String `tfsdk:"identifier"`
}

func registryVCSRepoAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"identifier": types.StringType}
}

// registryVCSRepoBlock is the vcs_repo block both registry resources share.
// The repository cannot change in place, and a change of case alone is not a change.
func registryVCSRepoBlock(description string) schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		MarkdownDescription: description,
		Attributes: map[string]schema.Attribute{
			"identifier": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The repository as `owner/name`. The owner becomes the namespace. The " +
					"environment's GitHub App has to be installed on it, or the apply fails with " +
					"`VCS_REPO_NOT_INSTALLED`. Changing it replaces the resource.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 200),
					stringvalidator.RegexMatches(regexp.MustCompile(registryRepoPattern), "must be a GitHub owner/name"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
							resp.RequiresReplace = !strings.EqualFold(req.PlanValue.ValueString(), req.StateValue.ValueString())
						},
						"A different repository replaces the resource.",
						"A different repository replaces the resource.",
					),
				},
			},
		},
		Validators: []validator.Object{
			objectvalidator.IsRequired(),
			objectvalidator.AlsoRequires(path.MatchRelative().AtName("identifier")),
		},
	}
}

// registryVCSRepoValue is the vcs_repo object for a repository the API
// returned, keeping the configured spelling when only the case differs.
func registryVCSRepoValue(current types.Object, repo *string) (types.Object, diag.Diagnostics) {
	if repo == nil || *repo == "" {
		return current, nil
	}
	if !current.IsNull() && !current.IsUnknown() {
		if configured, ok := current.Attributes()["identifier"].(types.String); ok && strings.EqualFold(configured.ValueString(), *repo) {
			return current, nil
		}
	}
	return types.ObjectValue(registryVCSRepoAttrTypes(), map[string]attr.Value{"identifier": types.StringValue(*repo)})
}

// registryVCSRepoIdentifier reads the identifier out of a planned vcs_repo block.
func registryVCSRepoIdentifier(ctx context.Context, value types.Object) (string, diag.Diagnostics) {
	var repo registryVCSRepoModel
	diags := value.As(ctx, &repo, basetypes.ObjectAsOptions{})
	return repo.Identifier.ValueString(), diags
}

// registryWriteDiagnostic renders a refused registry connect or delete with
// what to do about the API's error code.
func registryWriteDiagnostic(summary string, err error) diag.Diagnostic {
	hint := ""
	switch client.ErrorCode(err) {
	case client.VCSRepoNotInstalledCode:
		hint = "Install the environment's GitHub App on the repository, then apply again. "
	case client.RegistryModuleExistsCode, client.RegistryProviderExistsCode:
		hint = "Something already sits at this address. Import it with terraform import, or remove it first. "
	case client.RegistryInvalidModuleNameCode:
		hint = "Set name and provider, or name the repository terraform-<provider>-<name>. "
	case client.RegistryInvalidProviderNameCode:
		hint = "A provider repository has to be named terraform-provider-<type>. "
	case StepUpRequiredCode:
		hint = "A user session has to have signed in within 15 minutes to connect or delete. Use a wpk_ " +
			"agent key with registry:write, which is exempt, or sign in again. "
	case client.GitHubUnavailableCode:
		hint = "GitHub could not be reached. Apply again shortly. "
	}
	return diag.NewErrorDiagnostic(summary, hint+err.Error())
}

// resyncOnTriggerChange queues a resync when resync_triggers changed to a non
// empty value, which is how an in place update asks the registry to import
// tags or releases again.
func resyncOnTriggerChange(plan, state types.Map, resync func() error) diag.Diagnostics {
	var diags diag.Diagnostics
	if plan.IsNull() || plan.IsUnknown() || len(plan.Elements()) == 0 || plan.Equal(state) {
		return diags
	}
	if err := resync(); err != nil {
		diags.Append(apiDiagnostic("Cannot queue the resync", err))
	}
	return diags
}

// splitImportID splits an import id into exactly want slash separated parts.
func splitImportID(id string, want int) ([]string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != want {
		return nil, false
	}
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
	}
	return parts, true
}
