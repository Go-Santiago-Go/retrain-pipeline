# ------------------------------------------------------------------------------
# SageMaker execution role
#
# Assumed by the training job, not by CI. Separate so the job runs with the
# permissions the job needs rather than the permissions CI needs.
# ------------------------------------------------------------------------------

data "aws_iam_policy_document" "sagemaker_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["sagemaker.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sagemaker_execution" {
  name               = "${var.project_name}-sagemaker-execution"
  description        = "Assumed by the training job. Reads the input channel, writes artifacts and logs."
  assume_role_policy = data.aws_iam_policy_document.sagemaker_assume_role.json
}

# ------------------------------------------------------------------------------
# Execution role permissions
#
# Exactly the four things the job does: read the input dataset, write artifacts,
# write logs, pull the framework image. No registry, no DVC remote, no ability to
# start other jobs, because the job does none of those.
# ------------------------------------------------------------------------------

data "aws_iam_policy_document" "sagemaker_execution" {
  # ListBucket is a separate statement because it acts on the bucket itself, so its
  # resource is the bare ARN, while object actions target ARN/*.
  statement {
    sid       = "TrainingIO"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.model_artifacts.arn}/*"]
  }

  statement {
    sid       = "TrainingList"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.model_artifacts.arn]
  }

  # Without these the job runs blind: no log stream to read when it fails.
  statement {
    sid = "Logs"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:/aws/sagemaker/*"]
  }

  # GetAuthorizationToken is an account level login with no resource to scope to.
  statement {
    sid       = "PullImageAuth"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  # Read only pull of the AWS published sklearn image. Its ARN is region specific
  # and awkward to pin, so * is the accepted pattern for these read only actions.
  statement {
    sid = "PullImageLayers"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "sagemaker_execution" {
  name   = "${var.project_name}-sagemaker-execution"
  role   = aws_iam_role.sagemaker_execution.id
  policy = data.aws_iam_policy_document.sagemaker_execution.json
}
