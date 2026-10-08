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
	planSecretA = "arn:aws:secretsmanager:*:111122223333:secret:app-*"
	planSecretB = "arn:aws:secretsmanager:us-west-2:444455556666:secret:platform/app-??????"
)

// TestPlanSecretARNsUpdatePatch checks the PATCH body for each edit of the
// set, including the explicit null an emptied set sends.
func TestPlanSecretARNsUpdatePatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    []string
		plan     []string
		want     string
		response []string
	}{
		{name: "set", plan: []string{planSecretB, planSecretA}, want: `{"plan_secret_arns":["` + planSecretA + `","` + planSecretB + `"]}`, response: []string{planSecretA, planSecretB}},
		{name: "replace", prior: []string{planSecretA}, plan: []string{planSecretB}, want: `{"plan_secret_arns":["` + planSecretB + `"]}`, response: []string{planSecretB}},
		{name: "empty clears", prior: []string{planSecretA}, plan: []string{}, want: `{"plan_secret_arns":null}`},
		{name: "unchanged is omitted", prior: []string{planSecretA}, plan: []string{planSecretA}, want: `{}`, response: []string{planSecretA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prior := baseWorkspaceModel(t)
			prior.PlanSecretARNs = stringSet(t, tc.prior...)
			next := prior
			next.PlanSecretARNs = stringSet(t, tc.plan...)

			response := client.Workspace{
				WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
				PlanSecretARNs: tc.response,
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
			if !got.PlanSecretARNs.Equal(stringSet(t, tc.response...)) {
				t.Errorf("plan_secret_arns = %v, want %v", got.PlanSecretARNs, tc.response)
			}
		})
	}
}

// TestPlanSecretARNsCreate checks a create sends a configured set and omits an
// empty one.
func TestPlanSecretARNsCreate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		arns  []string
		check func(*testing.T, map[string]any)
	}{
		{name: "configured", arns: []string{planSecretA}, check: func(t *testing.T, body map[string]any) {
			if arns, _ := body["plan_secret_arns"].([]any); len(arns) != 1 || arns[0] != planSecretA {
				t.Errorf("plan_secret_arns = %v", body["plan_secret_arns"])
			}
		}},
		{name: "empty", arns: []string{}, check: func(t *testing.T, body map[string]any) {
			if _, ok := body["plan_secret_arns"]; ok {
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
					PlanSecretARNs: tc.arns,
				})
			})}
			model := baseWorkspaceModel(t)
			model.PlanSecretARNs = stringSet(t, tc.arns...)
			planned := resourceState(t, r, &model)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(planned)}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
		})
	}
}

// TestPlanSecretARNsValidators checks the schema refuses what the API refuses.
func TestPlanSecretARNsValidators(t *testing.T) {
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = fmt.Sprintf("arn:aws:secretsmanager:us-west-2:111122223333:secret:app-%d", i)
	}
	prefix := "arn:aws:secretsmanager:us-west-2:111122223333:secret:"
	long := prefix + strings.Repeat("s", 200-len(prefix)+1)
	for _, tc := range []struct {
		name  string
		arns  []string
		valid bool
	}{
		{name: "patterns", arns: []string{planSecretA, planSecretB}, valid: true},
		{name: "gov region", arns: []string{"arn:aws:secretsmanager:us-gov-west-1:111122223333:secret:app"}, valid: true},
		{name: "longest accepted", arns: []string{long[:200]}, valid: true},
		{name: "ten patterns", arns: eleven[:10], valid: true},
		{name: "account wildcard", arns: []string{"arn:aws:secretsmanager:us-west-2:*:secret:app"}},
		{name: "partial region wildcard", arns: []string{"arn:aws:secretsmanager:us-*:111122223333:secret:app"}},
		{name: "not a secret", arns: []string{"arn:aws:ssm:us-west-2:111122223333:parameter/app"}},
		{name: "other partition", arns: []string{"arn:aws-us-gov:secretsmanager:us-gov-west-1:111122223333:secret:app"}},
		{name: "surrounding whitespace", arns: []string{" " + planSecretA}},
		{name: "too long", arns: []string{long}},
		{name: "eleven patterns", arns: eleven},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			value := stringSet(t, tc.arns...)
			var diags []string
			for _, v := range planSecretARNsValidators() {
				out := &validator.SetResponse{}
				v.ValidateSet(ctx, validator.SetRequest{Path: path.Root("plan_secret_arns"), ConfigValue: value}, out)
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

// TestWorkspaceDataSourceReadsPlanSecretARNs checks the data source exposes the set.
func TestWorkspaceDataSourceReadsPlanSecretARNs(t *testing.T) {
	ctx := context.Background()
	d := &workspaceDataSource{client: contractClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Workspace{
			WorkspaceID: "ws-test", Name: "example", Engine: "terraform", EngineVersion: "1.9.8", CreatedAt: "created",
			PlanSecretARNs: []string{planSecretA},
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
	if diags := resp.State.GetAttribute(ctx, path.Root("plan_secret_arns"), &arns); diags.HasError() {
		t.Fatal(diags)
	}
	if !arns.Equal(stringSet(t, planSecretA)) {
		t.Errorf("plan_secret_arns = %v", arns)
	}
}

// TestPlanSecretARNsLifecycle drives set, replace, empty, re-set, removal and
// import through real Terraform plans against the fake API, checking an emptied
// or removed set leaves nothing stored.
func TestPlanSecretARNsLifecycle(t *testing.T) {
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
  name           = "secrets"
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
			_, ok := api.workspaces["ws-fake"]["plan_secret_arns"]
			if ok != want {
				return fmt.Errorf("plan_secret_arns stored = %v, want %v", ok, want)
			}
			return nil
		}
	}

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: config(fmt.Sprintf(`  plan_secret_arns = [%q]`, planSecretA)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_secret_arns.#", "1"),
					tfresource.TestCheckTypeSetElemAttr(address, "plan_secret_arns.*", planSecretA),
					stored(true),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_secret_arns = [%q, %q]`, planSecretB, planSecretA)),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_secret_arns.#", "2"),
					tfresource.TestCheckTypeSetElemAttr(address, "plan_secret_arns.*", planSecretB),
				),
			},
			{
				Config: config(`  plan_secret_arns = []`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_secret_arns.#", "0"),
					stored(false),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_secret_arns = [%q]`, planSecretB)),
				Check:  stored(true),
			},
			{
				Config: config(""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(address, "plan_secret_arns.#", "0"),
					stored(false),
				),
			},
			{
				Config:      config(`  plan_secret_arns = ["arn:aws:secretsmanager:us-west-2:*:secret:app"]`),
				ExpectError: regexp.MustCompile(`Secrets\s+Manager\s+ARN\s+pattern`),
			},
			{
				Config: config(fmt.Sprintf(`  plan_secret_arns = [%q]`, planSecretA)),
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
