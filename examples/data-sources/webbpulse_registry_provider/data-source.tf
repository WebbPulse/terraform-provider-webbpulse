data "webbpulse_registry_provider" "webbpulse" {
  namespace = "WebbPulse"
  type      = "webbpulse"
}

output "provider_versions" {
  value = data.webbpulse_registry_provider.webbpulse.published_versions
}
