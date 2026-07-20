# ------------------------------------------------------------------------------
# Project identity
#
# Values that are decisions about this project rather than facts about the
# environment. Facts, such as the account ID, are looked up in data.tf instead:
# hardcode decisions, look up facts.
# ------------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region for all resources in this project."
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Short project identifier. Prefixes resource names and appears in default tags."
  type        = string
  default     = "retrain-pipeline"
}

# ------------------------------------------------------------------------------
# GitHub OIDC subject
#
# These identify the single repository permitted to assume the CI role. They are
# interpolated into the role trust policy's condition on the token `sub` claim,
# which is the check that stops any other repository on GitHub from assuming it.
# ------------------------------------------------------------------------------

variable "github_org" {
  description = "GitHub owner of the repository permitted to assume the CI role via OIDC."
  type        = string
  default     = "Go-Santiago-Go"
}

variable "github_repo" {
  description = "Repository permitted to assume the CI role via OIDC."
  type        = string
  default     = "retrain-pipeline"
}

# Repos created after 2026-07-15 use immutable subject claims, which embed numeric
# IDs because a released name can be re-registered by someone else while an ID
# cannot. This repo was created 2026-07-20, so it is on the new format.
variable "github_owner_id" {
  description = "Numeric GitHub owner ID, from `gh api repos/OWNER/REPO --jq .owner.id`."
  type        = string
  default     = "85260356"
}

variable "github_repo_id" {
  description = "Numeric GitHub repository ID, from `gh api repos/OWNER/REPO --jq .id`."
  type        = string
  default     = "1306308958"
}
