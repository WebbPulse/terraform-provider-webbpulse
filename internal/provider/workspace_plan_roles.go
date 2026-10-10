package provider

import (
	"context"
	"regexp"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	planSecretARNsMax             = 10
	planSecretARNMaxLength        = 200
	planSecretARNPatternValue     = `^arn:aws:secretsmanager:(\*|[a-z]{2}(-[a-z]+)+-[0-9]):[0-9]{12}:secret:[A-Za-z0-9/_+=.@*?-]+$`
	planAssumeRoleARNsMax         = 10
	planAssumeRoleARNMaxLength    = 140
	planAssumeRoleARNPatternValue = `^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`
	planRoleARNMinLength          = 20
	planRoleARNMaxLength          = 140
)

var planAssumeRoleARNPattern = regexp.MustCompile(planAssumeRoleARNPatternValue)

var planSecretARNPattern = regexp.MustCompile(planSecretARNPatternValue)

// planSecretARNsValidators mirror the API's limits on plan_secret_arns: at most
// ten Secrets Manager ARN patterns of up to 200 characters, each pinned to one
// account, with wildcards allowed in the region and the name.
func planSecretARNsValidators() []validator.Set {
	return []validator.Set{
		setvalidator.SizeAtMost(planSecretARNsMax),
		setvalidator.ValueStringsAre(
			stringvalidator.LengthAtMost(planSecretARNMaxLength),
			stringvalidator.RegexMatches(
				planSecretARNPattern,
				"must be a Secrets Manager ARN pattern, arn:aws:secretsmanager:<region or *>:<12 digit account id>:secret:<name pattern>",
			),
		),
	}
}

// planRoleARNValidators mirror the API's limits on plan_role_arn: one exact IAM
// role ARN of 20 to 140 characters, no wildcards.
func planRoleARNValidators() []validator.String {
	return []validator.String{
		stringvalidator.LengthBetween(planRoleARNMinLength, planRoleARNMaxLength),
		stringvalidator.RegexMatches(
			planAssumeRoleARNPattern,
			"must be an exact IAM role ARN, arn:aws:iam::<12 digit account id>:role/<name>, with no wildcards",
		),
	}
}

// planAssumeRoleARNsValidators mirror the API's limits on plan_assume_role_arns:
// at most ten exact IAM role ARNs of up to 140 characters, no wildcards.
func planAssumeRoleARNsValidators() []validator.Set {
	return []validator.Set{
		setvalidator.SizeAtMost(planAssumeRoleARNsMax),
		setvalidator.ValueStringsAre(
			stringvalidator.LengthAtMost(planAssumeRoleARNMaxLength),
			stringvalidator.RegexMatches(
				planAssumeRoleARNPattern,
				"must be an exact IAM role ARN, arn:aws:iam::<12 digit account id>:role/<name>, with no wildcards",
			),
		),
	}
}

// planAssumeRoleARNsSet renders the API's list as a set, empty when the API
// returns null or omits the field.
func stringSetFromAPI(ctx context.Context, arns []string) (types.Set, diag.Diagnostics) {
	if arns == nil {
		arns = []string{}
	}
	return types.SetValueFrom(ctx, types.StringType, arns)
}

// setStrings reads a known set of strings in sorted order, or nil when it is
// null or unknown.
func setStrings(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	out := []string{}
	diags := set.ElementsAs(ctx, &out, false)
	sort.Strings(out)
	return out, diags
}

// planAssumeRoleARNsPatch is the merge patch value for plan_assume_role_arns:
// nil, so the key is omitted, when nothing changed, a pointer to a nil slice,
// which encodes as an explicit JSON null, when the set is empty or removed, and
// the full replacement list otherwise.
func stringSetPatch(ctx context.Context, plan, state types.Set) (*[]string, diag.Diagnostics) {
	if plan.IsUnknown() || plan.Equal(state) {
		return nil, nil
	}
	arns, diags := setStrings(ctx, plan)
	if len(arns) == 0 {
		arns = nil
	}
	return &arns, diags
}

// emptyStringSet is the default of an optional set attribute.
func emptyStringSet() types.Set {
	return types.SetValueMust(types.StringType, []attr.Value{})
}
