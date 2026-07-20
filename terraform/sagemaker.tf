# ------------------------------------------------------------------------------
# Model package group
#
# The registry container that Phase 6 registers model versions into. Free, and the
# approval state lives on each version, not here. The name matches the ARN pattern
# in CI's ModelRegistry permissions, so registration is not denied at runtime.
# ------------------------------------------------------------------------------

resource "aws_sagemaker_model_package_group" "models" {
  model_package_group_name        = "${var.project_name}-models"
  model_package_group_description = "Spam classifier versions. Each registers as PendingManualApproval with metrics and lineage."
}
