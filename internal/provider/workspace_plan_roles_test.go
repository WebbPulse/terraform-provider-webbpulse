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
	readerRoleA = "arn:aws:iam::111122223333:role/route53-reader"
	readerRoleB = "arn:aws:iam::444455556666:role/path/dns-reader"
)

func stringSet(t *testing.T, values ...string) types.Set {
	t.Helper()
	if values == nil {
		values = []string{}
	}
	set, diags := types.SetValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return set
}

// TestPlanAssumeRoleARNsUpdatePatch checks the PATCH body for each edit of the
// set, including the explicit null an emptied or removed set sends.
func TestPlanAssumeRoleARNsUpdatePatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    []string
		plan     []string
		want     string
		response []string
	}{
		{name: "set", plan: []string{readerRoleB, readerRoleA}, want: `{"plan_assume_role_arns":["` + readerRoleA + `","` + readerRoleB + `"]}`, response: []string{readerRoleA, readerRoleB}},
		{name: "replace", prior: []string{readerRoleA}, plan: []string{readerRoleB}, want: `{"plan_assume_role_arns":["` + readerRoleB + `"]}`, response: []string{readerRoleB}},
		{name: "empty clears", prior: []string{readerRoleA}, plan: []string{}, want: `{"plan_assume_role_arns":null}`},
		{name: "unchanged is omitted", prior: []string{readerRoleA}, plan: []string{readerRoleA}, want: `{}`, response: []string{readerRoleA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prior := baseWorkspaceModel(t)
			prior.PlanAssumeRoleARNs = stringSet(t, tc.prior...)
			next := prior
			next.PlanAssumeRoleARNs = stringSet(t, tc.plan...)

			response := client.Workspace{
				WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
				PlanAssumeRoleARNs: tc.response,
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
			if !got.PlanAssumeRoleARNs.Equal(stringSet(t, tc.response...)) {
				t.Errorf("plan_assume_role_arns = %v, want %v", got.PlanAssumeRoleARNs, tc.response)
			}
		})
	}
}

// TestPlanAssumeRoleARNsCreate checks a create sends a configured set and
// omits an empty one.
func TestPlanAssumeRoleARNsCreate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		arns  []string
		check func(*testing.T, map[string]any)
	}{
		{name: "configured", arns: []string{readerRoleA}, check: func(t *testing.T, body map[string]any) {
			if arns, _ := body["plan_assume_role_arns"].([]any); len(arns) != 1 || arns[0] != readerRoleA {
				t.Errorf("plan_assume_role_arns = %v", body["plan_assume_role_arns"])
			}
		}},
		{name: "empty", arns: []string{}, check: func(t *testing.T, body map[string]any) {
			if _, ok := body["plan_assume_role_arns"]; ok {
				t.Error("an empty set was sent on create")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &workspaceResource{client: contractClient(t, func(w http.ResponseWriter, req *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(req.Body).Decode(&body)
				tc.check(t, body)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(client.Workspace{
					WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
					PlanAssumeRoleARNs: tc.arns,
				})
			})}
			model := baseWorkspaceModel(t)
			model.PlanAssumeRoleARNs = stringSet(t, tc.arns...)
			planned := resourceState(t, r, &model)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}

// TestPlanAssumeRoleARNsValidators checks the schema refuses what the API refuses.
func TestPlanAssumeRoleARNsValidators(t *testing.T) {
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = fmt.Sprintf("arn:aws:iam::111122223333:role/reader-%d", i)
	}
	long := "arn:aws:iam::111122223333:role/" + strings.Repeat("r", 140-len("arn:aws:iam::111122223333:role/")+1)
	for _, tc := range []struct {
		name  string
		arns  []string
		valid bool
	}{
		{name: "exact roles", arns: []string{readerRoleA, readerRoleB}, valid: true},
		{name: "longest accepted", arns: []string{long[:140]}, valid: true},
		{name: "ten roles", arns: eleven[:10], valid: true},
		{name: "wildcard", arns: []string{"arn:aws:iam::111122223333:role/reader-*"}},
		{name: "account wildcard", arns: []string{"arn:aws:iam::*:role/reader"}},
		{name: "not a role", arns: []string{"arn:aws:iam::111122223333:user/reader"}},
		{name: "other partition", arns: []string{"arn:aws-us-gov:iam::111122223333:role/reader"}},
		{name: "surrounding whitespace", arns: []string{" " + readerRoleA}},
		{name: "141 characters", arns: []string{long}},
		{name: "eleven roles", arns: eleven},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			value := stringSet(t, tc.arns...)
			var diags []string
			for _, v := range planAssumeRoleARNsValidators() {
				out := &validator.SetResponse{}
				v.ValidateSet(ctx, validator.SetRequest{Path: path.Root("plan_assume_role_arns"), ConfigValue: value}, out)
				for _, d := range out.Diagnostics.Errors() {
					diags = append(diags, d.Detail())
				}
			}
			if tc.valid && len(diags) > 0 {
				t.Errorf("refused a valid set: %v", diags)
			}
			if !tc.valid && len(diags) == 0 {
				t.Error("accepted an invalid set")
			}
		})
	}
}

// TestWorkspaceDataSourceReadsPlanAssumeRoleARNs checks the data source exposes the set.
func TestWorkspaceDataSourceReadsPlanAssumeRoleARNs(t *testing.T) {
	ctx := context.Background()
	d := &workspaceDataSource{client: contractClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			PlanAssumeRoleARNs: []string{readerRoleA},
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
	var arns types.Set
	if diags := resp.State.GetAttribute(ctx, path.Root("plan_assume_role_arns"), &arns); diags.HasError() {
		t.Fatal(diags)
	}
	if !arns.Equal(stringSet(t, readerRoleA)) {
		t.Errorf("plan_assume_role_arns = %v", arns)
	}
}

// TestPlanAssumeRoleARNsLifecycle drives set, replace, empty, re-set, removal
// and import through real Terraform plans against the fake API, checking an
// emptied or removed set leaves nothing stored.
func TestPlanAssumeRoleARNsLifecycle(t *testing.T) {
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
  name           = "readers"
  engine_version = "1.9.8"
%s
}
`, server.URL, body)
	}
	const address = "webbpulse_workspace.test"
	stored := func(want bool) tfresource.TestCheckFunc {
		return func(_ *terraform.State) error {
			api.mu.Lock()
			defer api.mu.Unlock()
			_, ok := api.workspaces["ws-fake"]["plan_assume_role_arns"]
			if ok != want {
				return fmt.Errorf("plan_assume_role_arns stored = %v, want %v", ok, want)
			}
			return nil
		}
	}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q]`, readerRoleA)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "1"),
					tfresource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", readerRoleA),
					stored(true),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q, %q]`, readerRoleB, readerRoleA)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "2"),
					tfresource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", readerRoleB),
				),
			},
			{
				Config: config(`  plan_assume_role_arns = []`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "0"),
					stored(false),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q]`, readerRoleB)),
				Check:  stored(true),
			},
			{
				Config: config(""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "0"),
					stored(false),
				),
			},
			{
				Config:      config(`  plan_assume_role_arns = ["arn:aws:iam::111122223333:role/*"]`),
				ExpectError: regexp.MustCompile(`exact IAM role ARN`),
			},
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q]`, readerRoleA)),
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
