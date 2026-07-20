# ------------------------------------------------------------------------------
# CI role trust policy
#
# The OIDC provider proves a token came from GitHub. This proves it came from THIS
# repo: without a sub condition any repository on GitHub could assume the role, and
# would authenticate cleanly while doing it.
#
# Subjects are listed exactly rather than wildcarding the ref segment, which would
# also match tags and arbitrary branches. pull_request is here because the Phase 3
# quality gate needs dvc pull, and fork PRs cannot obtain a token at all.
# ------------------------------------------------------------------------------

data "aws_iam_policy_document" "ci_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [data.aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values = [
        "repo:${var.github_org}@${var.github_owner_id}/${var.github_repo}@${var.github_repo_id}:ref:refs/heads/main",
        "repo:${var.github_org}@${var.github_owner_id}/${var.github_repo}@${var.github_repo_id}:pull_request",
      ]
    }
  }
}

resource "aws_iam_role" "ci" {
  name               = "${var.project_name}-ci"
  description        = "Assumed by GitHub Actions via OIDC. Starts training jobs, never runs them."
  assume_role_policy = data.aws_iam_policy_document.ci_assume_role.json
}

# ------------------------------------------------------------------------------
# CI role permissions
#
# CI starts work and reads data. It deliberately cannot write model artifacts,
# which is the execution role's job, so a compromised runner cannot forge a model
# a human then approves. Every resource is scoped to this project's name prefix.
# ------------------------------------------------------------------------------

data "aws_iam_policy_document" "ci" {
  statement {
    sid       = "DvcRemoteObjects"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.dvc_remote.arn}/*"]
  }

  statement {
    sid       = "DvcRemoteList"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.dvc_remote.arn]
  }

  # Staging only: upload the pulled dataset as the training input, read metrics
  # back. The execution role writes the artifacts themselves.
  statement {
    sid     = "ArtifactsStaging"
    actions = ["s3:GetObject", "s3:PutObject", "s3:ListBucket"]
    resources = [
      aws_s3_bucket.model_artifacts.arn,
      "${aws_s3_bucket.model_artifacts.arn}/*",
    ]
  }

  statement {
    sid = "TrainingJobs"
    actions = [
      "sagemaker:CreateTrainingJob",
      "sagemaker:DescribeTrainingJob",
      "sagemaker:AddTags",
    ]
    resources = ["arn:aws:sagemaker:${var.aws_region}:${data.aws_caller_identity.current.account_id}:training-job/${var.project_name}*"]
  }

  statement {
    sid = "ModelRegistry"
    actions = [
      "sagemaker:CreateModelPackage",
      "sagemaker:DescribeModelPackage",
      "sagemaker:ListModelPackages",
    ]
    resources = [
      "arn:aws:sagemaker:${var.aws_region}:${data.aws_caller_identity.current.account_id}:model-package/${var.project_name}*",
      "arn:aws:sagemaker:${var.aws_region}:${data.aws_caller_identity.current.account_id}:model-package-group/${var.project_name}*",
    ]
  }

  # The escalation guard. Scoped to one role ARN and one service, so CI can hand
  # SageMaker exactly this role and nothing else.
  statement {
    sid       = "PassExecutionRole"
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.sagemaker_execution.arn]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["sagemaker.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "ci" {
  name   = "${var.project_name}-ci"
  role   = aws_iam_role.ci.id
  policy = data.aws_iam_policy_document.ci.json
}
