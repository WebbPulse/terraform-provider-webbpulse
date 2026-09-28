terraform {
  required_providers {
    webbpulse = {
      source  = "staging.terraform.webbpulse.com/WebbPulse/webbpulse"
      version = "0.2.0-rc.3"
    }
  }
}

provider "webbpulse" {
  host = "https://api.staging.terraform.webbpulse.com"
}
