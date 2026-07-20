# ------------------------------------------------------------------------------
# Provider configuration
#
# No credentials appear here. The provider resolves them from the standard chain
# (environment, shared config, assumed role), which is what lets this identical
# configuration run from a laptop and from a CI runner holding OIDC credentials.
# ------------------------------------------------------------------------------

provider "aws" {
  region = var.aws_region

  # Tagging at the provider level rather than per resource, so cost attribution and
  # ownership never depend on remembering to tag a new resource. A resource level
  # tags block still wins on key collision, making these a floor and not a ceiling.
  default_tags {
    tags = {
      Project   = var.project_name
      ManagedBy = "terraform"
      Repo      = "${var.github_org}/${var.github_repo}"
    }
  }
}
