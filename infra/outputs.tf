# ------------------------------------------------------------------------------
# Outputs
#
# Outputs are persisted into state, which is why adding one shows as a plan change
# even when no infrastructure moves. They are the public interface of this
# configuration: bucket names and role ARNs land here for trainctl and the GitHub
# Actions workflows to consume.
# ------------------------------------------------------------------------------

output "account_id" {
  description = "AWS account these resources are applied into."
  value       = data.aws_caller_identity.current.account_id
}

# Consumed by the train workflow's aws-actions/configure-aws-credentials step.
output "ci_role_arn" {
  description = "ARN of the CI role that GitHub Actions assumes via OIDC."
  value       = aws_iam_role.ci.arn
}

# Passed by trainctl as the RoleArn on CreateTrainingJob.
output "sagemaker_execution_role_arn" {
  description = "ARN of the role the training job runs as."
  value       = aws_iam_role.sagemaker_execution.arn
}

output "dvc_remote_bucket" {
  description = "Bucket backing the DVC S3 remote."
  value       = aws_s3_bucket.dvc_remote.bucket
}

output "model_artifacts_bucket" {
  description = "Bucket for model.tar.gz and metrics.json."
  value       = aws_s3_bucket.model_artifacts.bucket
}

output "tfstate_bucket" {
  description = "Bucket holding Terraform state, for the backend migration in the final Phase 1 step."
  value       = aws_s3_bucket.tfstate.bucket
}

output "model_package_group_name" {
  description = "Registry group trainctl register targets."
  value       = aws_sagemaker_model_package_group.models.model_package_group_name
}
