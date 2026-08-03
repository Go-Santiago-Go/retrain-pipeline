# ------------------------------------------------------------------------------
# Data sources
#
# Data sources read facts that already exist. They create and own nothing, so they
# never appear as changes in a plan.
# ------------------------------------------------------------------------------

# The account ID is a property of the credentials in use, not configuration. Reading
# it here keeps bucket names and role ARNs correct in whichever account applies this,
# rather than being correct in exactly one account.
data "aws_caller_identity" "current" {}
