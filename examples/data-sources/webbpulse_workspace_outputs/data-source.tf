data "webbpulse_workspace" "network" {
  name = "network"
}

data "webbpulse_workspace_outputs" "network" {
  workspace_id = data.webbpulse_workspace.network.workspace_id
}

output "vpc_id" {
  value = data.webbpulse_workspace_outputs.network.values.vpc_id
}
