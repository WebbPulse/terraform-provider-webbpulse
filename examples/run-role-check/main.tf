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
  name           = "example-run-role"
  engine_version = "1.9.8"
  run_role_arn   = "arn:aws:iam::111122223333:role/wp-tf-run-example"
}

data "webbpulse_run_role_check" "example" {
  workspace_id          = webbpulse_workspace.example.workspace_id
  fail_if_not_connected = false
}

output "run_role_status" {
  value       = data.webbpulse_run_role_check.example.status
  description = "connected, failed, or unverified until a plan-only run has tried the role."
}

output "run_role_connected" {
  value = data.webbpulse_run_role_check.example.connected
}

output "run_role_account_id" {
  value = data.webbpulse_run_role_check.example.account_id
}

output "run_role_error" {
  value = data.webbpulse_run_role_check.example.error
}

output "run_role_run_id" {
  value = data.webbpulse_run_role_check.example.run_id
}

output "run_role_checked_at" {
  value = data.webbpulse_run_role_check.example.checked_at
}
