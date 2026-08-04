# ------------------------------------------------------------------------------
# GitHub OIDC identity provider
#
# Account-global: exactly one per account, keyed by URL. It is shared across the
# portfolio repos, all of which authenticate GitHub Actions the same way, so a
# sibling project owns it and this config only references it for the ARN its trust
# policy needs. Owning it here would let this repo's destroy break the siblings.
# ------------------------------------------------------------------------------

data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}
