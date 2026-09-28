resource "webbpulse_workspace" "example" {
  name              = "example"
  engine_version    = "1.9.8"
  working_directory = "infra"

  vcs_repo {
    identifier = "WebbPulse/example-infra"
    branch     = "main"
  }
}
