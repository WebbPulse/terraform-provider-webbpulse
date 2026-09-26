terraform {
  required_providers {
    webbpulse = {
      source = "WebbPulse/webbpulse"
    }
  }
}

provider "webbpulse" {
  host = "https://api.staging.terraform.webbpulse.com"
}

resource "webbpulse_workspace" "example" {
  name              = "example"
  engine            = "terraform"
  engine_version    = "1.9.8"
  working_directory = "infra"
  description       = "Managed by the webbpulse provider"
  force_delete      = false
}

resource "webbpulse_variable" "region" {
  workspace_id = webbpulse_workspace.example.workspace_id
  key          = "region"
  value        = "us-west-2"
  category     = "terraform"
  description  = "The region this workspace deploys into"
}

resource "webbpulse_variable" "api_token" {
  workspace_id = webbpulse_workspace.example.workspace_id
  key          = "API_TOKEN"
  value        = var.api_token
  category     = "env"
  sensitive    = true
}

output "run_role_setup" {
  value       = webbpulse_workspace.example.run_role_setup
  description = "Build the run role from these values, then set run_role_arn on the workspace."
}
