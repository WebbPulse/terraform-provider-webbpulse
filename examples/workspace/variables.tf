variable "api_token" {
  type        = string
  sensitive   = true
  description = "A token the workspace's runs need as a process environment variable."
}

variable "vcs_repo" {
  type        = string
  description = "The GitHub repository, as owner/name, the environment's GitHub App is installed on."
}
