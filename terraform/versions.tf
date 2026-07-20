# ------------------------------------------------------------------------------
# Tooling contract
#
# Two independently versioned things are pinned here: the Terraform binary and the
# AWS provider plugin, which ships on its own schedule. The `~>` operator accepts
# non-breaking releases and blocks the next major, so absorbing a breaking provider
# release is always a deliberate upgrade rather than an accident of timing.
#
# The constraint says what is acceptable. .terraform.lock.hcl records what was
# actually selected, with checksums, and is committed for that reason.
# ------------------------------------------------------------------------------

terraform {
  required_version = "~> 1.15"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}
