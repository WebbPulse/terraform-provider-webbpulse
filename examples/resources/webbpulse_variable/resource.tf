resource "webbpulse_variable" "region" {
  workspace_id = webbpulse_workspace.example.workspace_id
  key          = "region"
  value        = "us-west-2"
  category     = "terraform"
}

resource "webbpulse_variable" "regions" {
  workspace_id = webbpulse_workspace.example.workspace_id
  key          = "regions"
  value        = jsonencode(["us-west-2", "eu-west-1"])
  hcl          = true
}
