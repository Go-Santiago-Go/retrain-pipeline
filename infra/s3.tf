# ------------------------------------------------------------------------------
# Buckets
#
# S3 bucket names occupy a single global namespace shared by every AWS account, so
# they are suffixed with the account ID to guarantee uniqueness. The account ID is
# not sensitive.
# ------------------------------------------------------------------------------

# Holds Terraform's own state, separate from the application buckets so that data
# lifecycle operations there can never touch state. Teardown needs state migrated
# out first, since destroying this bucket would destroy the state describing it.
resource "aws_s3_bucket" "tfstate" {
  bucket = "${var.project_name}-tfstate-${data.aws_caller_identity.current.account_id}"
}

# DVC remote. Holds content-addressed dataset blobs pushed by a human and pulled by CI.
resource "aws_s3_bucket" "dvc_remote" {
  bucket = "${var.project_name}-dvc-remote-${data.aws_caller_identity.current.account_id}"
}

# SageMaker training job outputs: model.tar.gz and metrics.json.
resource "aws_s3_bucket" "model_artifacts" {
  bucket = "${var.project_name}-model-artifacts-${data.aws_caller_identity.current.account_id}"
}

# ------------------------------------------------------------------------------
# Public access
#
# Blocking is on by default for new buckets, but is declared explicitly so the
# posture is visible to anyone reading the repo and survives a change in AWS
# defaults. Nothing in this project is ever served publicly from S3.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_public_access_block" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "dvc_remote" {
  bucket = aws_s3_bucket.dvc_remote.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "model_artifacts" {
  bucket = aws_s3_bucket.model_artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------------------
# Encryption at rest
#
# SSE-S3 rather than SSE-KMS. KMS would buy a key level audit trail this project
# has no consumer for, and add a second authorization gate whose failures surface
# as S3 AccessDenied errors pointing at the wrong subsystem.
#
# On by default since January 2023, so these change nothing functionally. Declared
# so the posture is visible to a reader and survives a change in AWS defaults.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_server_side_encryption_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "dvc_remote" {
  bucket = aws_s3_bucket.dvc_remote.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "model_artifacts" {
  bucket = aws_s3_bucket.model_artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# ------------------------------------------------------------------------------
# Versioning
#
# Versioning is undo for overwrites, so the test is whether anything writes a
# different value to the same key. Only state does. The other two are unversioned
# deliberately: DVC keys are content hashes and job names derive from dataset hash
# plus Git SHA, so both are write once by construction.
#
# Disabled is declared rather than omitted, so the decision is visible to a reader.
# Note it is a one way door: the S3 API cannot return a bucket to unversioned, so
# Enabled to Disabled errors.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_versioning" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_versioning" "dvc_remote" {
  bucket = aws_s3_bucket.dvc_remote.id

  versioning_configuration {
    status = "Disabled"
  }
}

resource "aws_s3_bucket_versioning" "model_artifacts" {
  bucket = aws_s3_bucket.model_artifacts.id

  versioning_configuration {
    status = "Disabled"
  }
}

# ------------------------------------------------------------------------------
# Lifecycle
#
# Both rules here clean up storage that bills at full rates while being invisible
# in a default bucket listing, which is what makes it a classic surprise.
#
# Incomplete multipart uploads have no expiry: parts bill at full rates forever and
# never appear in a normal object listing. Nothing here is currently large enough to
# trigger multipart at all, since boto3's threshold is 8MB and the biggest object is
# a few MB. The rule is here because the training set grows by design, the threshold
# is a client default outside our control, and the failure is silent and unbounded.
# One day, since anything unfinished by then is abandoned rather than slow.
#
# Noncurrent version expiry applies only to state, the one versioned bucket. Seven
# days, because a bad apply on a single operator project is caught in minutes, and
# rolling back to state older than a week would be its own incident.
# ------------------------------------------------------------------------------

resource "aws_s3_bucket_lifecycle_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  # The lifecycle rules read the bucket's versioning state, which no attribute
  # reference expresses, so the ordering has to be declared explicitly.
  depends_on = [aws_s3_bucket_versioning.tfstate]

  rule {
    id     = "expire-noncurrent-state-versions"
    status = "Enabled"
    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 7
    }
  }

  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "dvc_remote" {
  bucket = aws_s3_bucket.dvc_remote.id

  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "model_artifacts" {
  bucket = aws_s3_bucket.model_artifacts.id

  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}
