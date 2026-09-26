package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccProtoV6ProviderFactories serves this provider in process to the test
// framework, so an acceptance test needs no installed provider binary.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"webbpulse": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck skips unless TF_ACC and both credentials are set. Acceptance
// tests create real workspaces, so CI leaves them off and they are never
// pointed at an environment whose resources matter.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests are off: set TF_ACC=1 to run them")
	}
	for _, name := range []string{EnvHost, EnvToken} {
		if os.Getenv(name) == "" {
			t.Skipf("acceptance tests need %s", name)
		}
	}
}

func testAccName(prefix string) string {
	return fmt.Sprintf("tfacc-%s-%d", prefix, time.Now().UnixNano())
}

// TestAccWorkspace exercises the workspace lifecycle, clearing and import against a live API.
func TestAccWorkspace(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("workspace")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
  description    = "created by an acceptance test"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "name", name),
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "engine", "terraform"),
					resource.TestCheckResourceAttrSet("webbpulse_workspace.test", "workspace_id"),
					resource.TestCheckResourceAttrSet("webbpulse_workspace.test", "created_at"),
					resource.TestCheckResourceAttrSet("webbpulse_workspace.test", "run_role_setup.external_id"),
					resource.TestCheckResourceAttrSet("webbpulse_workspace.test", "run_role_setup.role_name"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name              = %q
  engine_version    = "1.10.0"
  description       = "edited by an acceptance test"
  working_directory = "infra"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "engine_version", "1.10.0"),
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "working_directory", "infra"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.10.0"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "description", ""),
					resource.TestCheckResourceAttr("webbpulse_workspace.test", "working_directory", ""),
					resource.TestCheckNoResourceAttr("webbpulse_workspace.test", "run_role_arn"),
				),
			},
			{
				ResourceName:                         "webbpulse_workspace.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "workspace_id",
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					rs, ok := state.RootModule().Resources["webbpulse_workspace.test"]
					if !ok {
						return "", fmt.Errorf("the workspace is not in state")
					}
					return rs.Primary.Attributes["workspace_id"], nil
				},
			},
		},
	})
}

// TestAccVariable exercises plain and sensitive variables against a live API.
func TestAccVariable(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("variable")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}

resource "webbpulse_variable" "plain" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "region"
  value        = "us-west-2"
  category     = "terraform"
}

resource "webbpulse_variable" "secret" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "API_TOKEN"
  value        = "not-a-real-token"
  category     = "env"
  sensitive    = true
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("webbpulse_variable.plain", "value", "us-west-2"),
					resource.TestCheckResourceAttr("webbpulse_variable.secret", "sensitive", "true"),
					resource.TestCheckResourceAttr("webbpulse_variable.secret", "category", "env"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}

resource "webbpulse_variable" "plain" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "region"
  value        = "eu-west-1"
  category     = "terraform"
}

resource "webbpulse_variable" "secret" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "API_TOKEN"
  value        = "not-a-real-token"
  category     = "env"
  sensitive    = true
}
`, name),
				Check: resource.TestCheckResourceAttr("webbpulse_variable.plain", "value", "eu-west-1"),
			},
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}

resource "webbpulse_variable" "plain" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "region"
  value        = "eu-west-1"
  category     = "terraform"
}

resource "webbpulse_variable" "secret" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "API_TOKEN"
  value        = "not-a-real-token"
  category     = "env"
  sensitive    = true
}
`, name),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// TestAccWorkspaceDataSource looks a workspace up by id and by name against a live API.
func TestAccWorkspaceDataSource(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("datasource")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}

data "webbpulse_workspace" "by_id" {
  workspace_id = webbpulse_workspace.test.workspace_id
}

data "webbpulse_workspace" "by_name" {
  name       = webbpulse_workspace.test.name
  depends_on = [webbpulse_workspace.test]
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.webbpulse_workspace.by_id", "name", name),
					resource.TestCheckResourceAttr("data.webbpulse_workspace.by_name", "name", name),
					resource.TestCheckResourceAttrPair(
						"data.webbpulse_workspace.by_id", "workspace_id",
						"webbpulse_workspace.test", "workspace_id",
					),
				),
			},
		},
	})
}
