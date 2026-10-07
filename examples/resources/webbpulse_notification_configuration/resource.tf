variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

variable "alerts_signing_token" {
  type      = string
  sensitive = true
}

resource "webbpulse_notification_configuration" "slack" {
  workspace_id     = webbpulse_workspace.example.workspace_id
  name             = "deploys"
  destination_type = "slack"
  url              = var.slack_webhook_url
  triggers         = ["run:needs_attention", "run:completed", "run:errored"]
}

resource "webbpulse_notification_configuration" "alerts" {
  workspace_id     = webbpulse_workspace.example.workspace_id
  name             = "alerts"
  destination_type = "generic"
  url              = "https://alerts.example.com/terraform"
  token            = var.alerts_signing_token
  triggers         = ["run:errored"]
}
