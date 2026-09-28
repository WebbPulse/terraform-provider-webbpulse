package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccProtoV6ProviderFactories serves this provider in process to the test
// framework, so an acceptance test needs no installed provider binary.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"webbpulse": providerserver.NewProtocol6WithError(testAccProvider()),
}

// testAccProvider is the provider under test. It reads the access gate value
// from the same environment variable a real configuration falls back to.
func testAccProvider() provider.Provider {
	return &webbpulseProvider{version: "test"}
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

// TestAccVariable exercises plain, sensitive and HCL variables against a live
// API, including an HCL list that round-trips, imports and changes in place.
func TestAccVariable(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("variable")
	const listAddress = "webbpulse_variable.list"
	config := func(region string, listHCL bool) string {
		return fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}

resource "webbpulse_variable" "plain" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "region"
  value        = %q
  category     = "terraform"
}

resource "webbpulse_variable" "secret" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "API_TOKEN"
  value        = "not-a-real-token"
  category     = "env"
  sensitive    = true
}

resource "webbpulse_variable" "list" {
  workspace_id = webbpulse_workspace.test.workspace_id
  key          = "regions"
  value        = jsonencode(["us-west-2", "eu-west-1"])
  hcl          = %t
}
`, name, region, listHCL)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("us-west-2", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("webbpulse_variable.plain", "value", "us-west-2"),
					resource.TestCheckResourceAttr("webbpulse_variable.plain", "hcl", "false"),
					resource.TestCheckResourceAttr("webbpulse_variable.secret", "sensitive", "true"),
					resource.TestCheckResourceAttr("webbpulse_variable.secret", "category", "env"),
					resource.TestCheckResourceAttr(listAddress, "hcl", "true"),
					resource.TestCheckResourceAttr(listAddress, "category", "terraform"),
					resource.TestCheckResourceAttr(listAddress, "value", `["us-west-2","eu-west-1"]`),
				),
			},
			{
				Config: config("eu-west-1", true),
				Check:  resource.TestCheckResourceAttr("webbpulse_variable.plain", "value", "eu-west-1"),
			},
			{
				Config:             config("eu-west-1", true),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
			{
				ResourceName:                         listAddress,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "key",
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					rs, ok := state.RootModule().Resources[listAddress]
					if !ok {
						return "", fmt.Errorf("the list variable is not in state")
					}
					return rs.Primary.Attributes["workspace_id"] + "/" + rs.Primary.Attributes["key"], nil
				},
			},
			{
				Config: config("eu-west-1", false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(listAddress, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(listAddress, "hcl", "false"),
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

// EnvAccVCSRepo names a GitHub owner/name the target environment's GitHub App
// is installed on. The VCS acceptance test skips without it, since connecting a
// repository needs an App installation the test cannot create.
const EnvAccVCSRepo = "WEBBPULSE_TF_ACC_VCS_REPO"

// TestAccWorkspaceTriggerSettings sets and clears the trigger settings against a live API.
func TestAccWorkspaceTriggerSettings(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("triggers")
	const address = "webbpulse_workspace.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name                  = %q
  engine_version        = "1.9.8"
  working_directory     = "infra"
  trigger_patterns      = ["/modules/**", "*.tf"]
  file_triggers_enabled = false
  speculative_enabled   = false
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "trigger_patterns.#", "2"),
					resource.TestCheckResourceAttr(address, "file_triggers_enabled", "false"),
					resource.TestCheckResourceAttr(address, "speculative_enabled", "false"),
					resource.TestCheckNoResourceAttr(address, "vcs_repo.identifier"),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "trigger_patterns.#", "0"),
					resource.TestCheckResourceAttr(address, "file_triggers_enabled", "true"),
					resource.TestCheckResourceAttr(address, "speculative_enabled", "true"),
				),
			},
		},
	})
}

// TestAccWorkspaceVCS connects, retargets and disconnects a repository against
// a live API, and checks a repository the App cannot see is refused.
func TestAccWorkspaceVCS(t *testing.T) {
	testAccPreCheck(t)
	repo := os.Getenv(EnvAccVCSRepo)
	if repo == "" {
		t.Skipf("the VCS acceptance test needs %s, a repository the environment's GitHub App is installed on", EnvAccVCSRepo)
	}

	name := testAccName("vcs")
	const address = "webbpulse_workspace.test"
	config := func(block string) string {
		return fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
%s
}

data "webbpulse_workspace" "test" {
  workspace_id = webbpulse_workspace.test.workspace_id
}
`, name, block)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(fmt.Sprintf(`
  vcs_repo {
    identifier = %q
  }
`, repo)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "vcs_repo.identifier", repo),
					resource.TestCheckResourceAttrSet(address, "vcs_repo.branch"),
					resource.TestCheckResourceAttrSet(address, "vcs_repo.repository_id"),
					resource.TestCheckResourceAttrSet(address, "vcs_repo.installation_id"),
					resource.TestCheckResourceAttrPair("data.webbpulse_workspace.test", "vcs_repo.branch", address, "vcs_repo.branch"),
				),
			},
			{
				Config: config(fmt.Sprintf(`
  vcs_repo {
    identifier = %q
    branch     = "tfacc-branch"
  }
`, repo)),
				Check: resource.TestCheckResourceAttr(address, "vcs_repo.branch", "tfacc-branch"),
			},
			{
				Config: config(fmt.Sprintf(`
  vcs_repo {
    identifier = %q
  }
`, "WebbPulse/tfacc-not-installed-"+name)),
				ExpectError: regexp.MustCompile(`VCS_REPO_NOT_INSTALLED`),
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "vcs_repo.identifier"),
					resource.TestCheckNoResourceAttr("data.webbpulse_workspace.test", "vcs_repo.identifier"),
				),
			},
		},
	})
}

// TestAccWorkspacePlanAssumeRoleARNs sets, replaces, empties, re-sets, removes
// and imports plan_assume_role_arns against a live API, reading it back through
// the data source. The ARNs are exact but name no real roles, which the API
// does not require.
func TestAccWorkspacePlanAssumeRoleARNs(t *testing.T) {
	testAccPreCheck(t)

	name := testAccName("planroles")
	const address = "webbpulse_workspace.test"
	const roleA = "arn:aws:iam::111122223333:role/tfacc-route53-reader"
	const roleB = "arn:aws:iam::444455556666:role/tfacc/dns-reader"
	config := func(arns string) string {
		return fmt.Sprintf(`
resource "webbpulse_workspace" "test" {
  name           = %q
  engine_version = "1.9.8"
%s
}

data "webbpulse_workspace" "test" {
  workspace_id = webbpulse_workspace.test.workspace_id
}
`, name, arns)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		PreCheck:                 func() { testAccPreCheck(t) },
		Steps: []resource.TestStep{
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q, %q]`, roleA, roleB)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", roleA),
					resource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", roleB),
					resource.TestCheckResourceAttr("data.webbpulse_workspace.test", "plan_assume_role_arns.#", "2"),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q]`, roleB)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "1"),
					resource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", roleB),
				),
			},
			{
				Config: config(`  plan_assume_role_arns = []`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "0"),
					resource.TestCheckResourceAttr("data.webbpulse_workspace.test", "plan_assume_role_arns.#", "0"),
				),
			},
			{
				Config: config(fmt.Sprintf(`  plan_assume_role_arns = [%q]`, roleA)),
				Check:  resource.TestCheckTypeSetElemAttr(address, "plan_assume_role_arns.*", roleA),
			},
			{
				ResourceName:                         address,
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "workspace_id",
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					rs, ok := state.RootModule().Resources[address]
					if !ok {
						return "", fmt.Errorf("the workspace is not in state")
					}
					return rs.Primary.Attributes["workspace_id"], nil
				},
			},
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "plan_assume_role_arns.#", "0"),
					resource.TestCheckResourceAttr("data.webbpulse_workspace.test", "plan_assume_role_arns.#", "0"),
				),
			},
			{
				Config:      config(`  plan_assume_role_arns = ["arn:aws:iam::111122223333:role/tfacc-*"]`),
				ExpectError: regexp.MustCompile(`exact IAM role ARN`),
			},
		},
	})
}
