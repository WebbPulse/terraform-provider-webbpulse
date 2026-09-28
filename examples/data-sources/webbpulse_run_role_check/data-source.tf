data "webbpulse_run_role_check" "example" {
  workspace_id          = webbpulse_workspace.example.workspace_id
  fail_if_not_connected = true
}
