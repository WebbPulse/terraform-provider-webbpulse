resource "webbpulse_project" "platform" {
  name        = "Platform"
  description = "Shared networking and DNS"
}

resource "webbpulse_workspace" "dns" {
  name           = "dns"
  engine_version = "1.9.8"
  project_id     = webbpulse_project.platform.id
}
