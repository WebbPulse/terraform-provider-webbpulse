package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	planRoleA = "arn:aws:iam::111122223333:role/example-plan"
	planRoleB = "arn:aws:iam::444455556666:role/platform/example-plan"
)

// optionalStringValue is a null string for "" and the value otherwise.
func optionalStringValue(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// TestPlanRoleARNUpdatePatch checks the PATCH body for each edit of the role,
// including the explicit null a removed role sends.
func TestPlanRoleARNUpdatePatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    string
		plan     string
		want     string
		response string
	}{
		{name: "set", plan: planRoleA, want: `{"plan_role_arn":"` + planRoleA + `"}`, response: planRoleA},
		{name: "replace", prior: planRoleA, plan: planRoleB, want: `{"plan_role_arn":"` + planRoleB + `"}`, response: planRoleB},
		{name: "removal clears", prior: planRoleA, want: `{"plan_role_arn":null}`},
		{name: "unchanged is omitted", prior: planRoleA, plan: planRoleA, want: `{}`, response: planRoleA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prior := baseWorkspaceModel(t)
			prior.PlanRoleARN = optionalStringValue(tc.prior)
			next := prior
			next.PlanRoleARN = optionalStringValue(tc.plan)

			response := client.Workspace{
				WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			}
			if tc.response != "" {
				response.PlanRoleARN = &tc.response
			}
			r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				var body json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if string(body) != tc.want {
					t.Errorf("PATCH body = %s, want %s", body, tc.want)
				}
				_ = json.NewEncoder(w).Encode(response)
			})}

			priorState := resourceState(t, r, &prior)
			planned := resourceState(t, r, &next)
			resp := resource.UpdateResponse{State: priorState}
			r.Update(ctx, resource.UpdateRequest{State: priorState, Plan: tfsdk.Plan(planned)}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got workspaceModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatal(diags)
			}
			if !got.PlanRoleARN.Equal(optionalStringValue(tc.response)) {
				t.Errorf("plan_role_arn = %v, want %q", got.PlanRoleARN, tc.response)
			}
		})
	}
}

// TestPlanRoleARNCreate checks a create sends a configured role and omits an
// unset one.
func TestPlanRoleARNCreate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		arn   string
		check func(*testing.T, map[string]any)
	}{
		{name: "configured", arn: planRoleA, check: func(t *testing.T, body map[string]any) {
			if body["plan_role_arn"] != planRoleA {
				t.Errorf("plan_role_arn = %v", body["plan_role_arn"])
			}
		}},
		{name: "unset", check: func(t *testing.T, body map[string]any) {
			if _, ok := body["plan_role_arn"]; ok {
				t.Error("an unset role was sent on create")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(req.Body).Decode(&body)
				tc.check(t, body)
				created := client.Workspace{
					WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
				}
				if tc.arn != "" {
					created.PlanRoleARN = &tc.arn
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(created)
			})}
			model := baseWorkspaceModel(t)
			model.PlanRoleARN = optionalStringValue(tc.arn)
			planned := resourceState(t, r, &model)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got workspaceModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatal(diags)
			}
			if !got.PlanRoleARN.Equal(optionalStringValue(tc.arn)) {
				t.Errorf("plan_role_arn = %v, want %q", got.PlanRoleARN, tc.arn)
			}
		})
	}
}

// TestPlanRoleARNValidators checks the schema refuses what the API refuses.
func TestPlanRoleARNValidators(t *testing.T) {
	prefix := "arn:aws:iam::111122223333:role/"
	long := prefix + strings.Repeat("r", 140-len(prefix)+1)
	for _, tc := range []struct {
		name  string
		arn   string
		valid bool
	}{
		{name: "role", arn: planRoleA, valid: true},
		{name: "role with a path", arn: planRoleB, valid: true},
		{name: "longest accepted", arn: long[:140], valid: true},
		{name: "too long", arn: long},
		{name: "wildcard", arn: "arn:aws:iam::111122223333:role/plan-*"},
		{name: "account wildcard", arn: "arn:aws:iam::*:role/example-plan"},
		{name: "not a role", arn: "arn:aws:iam::111122223333:user/example"},
		{name: "other partition", arn: "arn:aws-us-gov:iam::111122223333:role/example-plan"},
		{name: "surrounding whitespace", arn: " " + planRoleA},
		{name: "empty", arn: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var diags []string
			for _, v := range planRoleARNValidators() {
				out := &validator.StringResponse{}
				v.ValidateString(ctx, validator.StringRequest{Path: path.Root("plan_role_arn"), ConfigValue: types.StringValue(tc.arn)}, out)
				for _, d := range out.Diagnostics.Errors() {
					diags = append(diags, d.Detail())
				}
			}
			if tc.valid && len(diags) > 0 {
				t.Errorf("refused a valid role: %v", diags)
			}
			if !tc.valid && len(diags) == 0 {
				t.Error("accepted an invalid role")
			}
		})
	}
}

// TestWorkspaceDataSourceReadsPlanRoleARN checks the data source exposes the role.
func TestWorkspaceDataSourceReadsPlanRoleARN(t *testing.T) {
	ctx := context.Background()
	arn := planRoleA
	d := &workspaceDataSource{client: contractClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			PlanRoleARN: &arn,
		})
	})}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objectType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	values["workspace_id"] = tftypes.NewValue(tftypes.String, "ws-test")
	config := tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objectType, values)}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: config}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("plan_role_arn"), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.ValueString() != planRoleA {
		t.Errorf("plan_role_arn = %v", got)
	}
}

// TestPlanRoleARNLifecycle drives set, change, removal, re-set and import
// through real Terraform plans against the fake API, checking a removed role
// leaves nothing stored.
func TestPlanRoleARNLifecycle(t *testing.T) {
	api := &fakeWorkspaceAPI{workspaces: map[string]map[string]any{}}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	config := func(body string) string {
		return fmt.Sprintf(`
provider "webbpulse" {
  host  = %q
  token = "synthetic-test-only"
}

resource "webbpulse_workspace" "test" {
  name           = "planrole"
  engine_version = "1.9.8"
%s
}
`, server.URL, body)
	}
	const address = "webbpulse_workspace.test"
	stored := func(want string) tfresource.TestCheckFunc {
		return func(_ *terraform.State) error {
			api.mu.Lock()
			defer api.mu.Unlock()
			got, ok := api.workspaces["ws-fake"]["plan_role_arn"]
			if want == "" && ok {
				return fmt.Errorf("plan_role_arn stored = %v, want none", got)
			}
			if want != "" && got != want {
				return fmt.Errorf("plan_role_arn stored = %v, want %s", got, want)
			}
			return nil
		}
	}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: config(fmt.Sprintf(`  plan_role_arn = %q`, planRoleA)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_role_arn", planRoleA),
					stored(planRoleA),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_role_arn = %q`, planRoleB)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_role_arn", planRoleB),
					stored(planRoleB),
				),
			},
			{
				Config: config(""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckNoResourceAttr(address, "plan_role_arn"),
					stored(""),
				),
			},
			{
				Config:      config(`  plan_role_arn = "arn:aws:iam::111122223333:role/plan-*"`),
				ExpectError: regexp.MustCompile(`exact\s+IAM\s+role\s+ARN`),
			},
			{
				Config: config(fmt.Sprintf(`  plan_role_arn = %q`, planRoleA)),
				Check:  stored(planRoleA),
			},
			{
				ResourceName:                         address,
				ImportState:                          true,
				ImportStateId:                        "ws-fake",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "workspace_id",
				ImportStateVerifyIgnore:              []string{"force_delete"},
			},
		},
	})
}
