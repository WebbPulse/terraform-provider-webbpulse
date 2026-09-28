resource "webbpulse_workspace" "example" {
  name              = "example"
  engine_version    = "1.9.8"
  working_directory = "infra"

  plan_assume_role_arns = [
    "arn:aws:iam::111122223333:role/route53-reader",
  ]

  vcs_repo {
    identifier = "WebbPulse/example-infra"
    branch     = "main"
  }
}
