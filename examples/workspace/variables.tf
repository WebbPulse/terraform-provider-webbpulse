variable "api_token" {
  type        = string
  sensitive   = true
  description = "A token the workspace's runs need as a process environment variable."
}
