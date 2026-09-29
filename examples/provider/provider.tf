terraform {
  required_providers {
    webbpulse = {
      source  = "staging.terraform.webbpulse.com/WebbPulse/webbpulse"
      version = "0.2.0-rc.5"
    }
  }
}

provider "webbpulse" {
  host                        = "https://api.staging.terraform.webbpulse.com"
  origin_verify_ssm_parameter = "/webbpulse-terraform-stg/access-gate/origin-verify"
}
