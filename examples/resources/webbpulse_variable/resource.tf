resource "webbpulse_variable" "region" {
  workspace_id = webbpulse_workspace.example.workspace_id
  key          = "region"
  value        = "us-west-2"
  category     = "terraform"
}
