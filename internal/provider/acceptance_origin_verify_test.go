package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// EnvAccGateParameter names the SSM parameter holding the staging access gate
// value for TestAccOriginVerifyFromSSMParameter.
const EnvAccGateParameter = "WEBBPULSE_TF_ACC_GATE_PARAMETER"

// TestAccOriginVerifyFromSSMParameter drives the gated staging API with the
// gate value read from SSM by the provider itself, and checks the value never
// reaches state.
func TestAccOriginVerifyFromSSMParameter(t *testing.T) {
	testAccPreCheck(t)
	parameter := os.Getenv(EnvAccGateParameter)
	if parameter == "" {
		t.Skipf("the SSM origin_verify acceptance test needs %s", EnvAccGateParameter)
	}

	direct := os.Getenv(EnvOriginVerify)
	t.Setenv(EnvOriginVerify, "")
	t.Setenv(EnvOriginVerifySSMParameter, "")

	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttrSet("webbpulse_workspace.test", "workspace_id"),
	}
	if direct != "" {
		checks = append(checks, stateNeverHolds(direct))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "webbpulse" {
  origin_verify_ssm_parameter = %q
}

resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
  description    = "created by the SSM origin_verify acceptance test"
}
`, parameter, testAccName("gate-ssm")),
			Check: resource.ComposeAggregateTestCheckFunc(checks...),
		}},
	})
}
