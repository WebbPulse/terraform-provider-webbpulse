resource "webbpulse_registry_module" "network" {
  vcs_repo {
    identifier = "WebbPulse/terraform-aws-network"
  }
}

resource "webbpulse_registry_module" "named" {
  name            = "baseline"
  module_provider = "aws"
  import_tags     = true

  resync_triggers = {
    round = "1"
  }

  vcs_repo {
    identifier = "WebbPulse/infra-modules"
  }
}
