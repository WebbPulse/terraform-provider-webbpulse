data "webbpulse_registry_module" "network" {
  namespace       = "WebbPulse"
  name            = "network"
  module_provider = "aws"
}

output "network_versions" {
  value = data.webbpulse_registry_module.network.published_versions
}
