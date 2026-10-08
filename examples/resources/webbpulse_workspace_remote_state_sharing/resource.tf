resource "webbpulse_workspace_remote_state_sharing" "network" {
  workspace_id              = webbpulse_workspace.network.workspace_id
  remote_state_consumer_ids = [webbpulse_workspace.app.workspace_id]
}
