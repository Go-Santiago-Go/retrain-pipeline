# ------------------------------------------------------------------------------
# Remote state
#
# State lives in the tfstate bucket this same config created, migrated in after
# the first local-state apply (the bootstrap chicken-and-egg). Values are literal
# because backend config is read before variables are evaluated.
#
# use_lockfile gives native S3 locking, so no DynamoDB table is needed. Teardown
# must migrate state back to local first, since destroy would otherwise delete the
# bucket holding the state it is using.
# ------------------------------------------------------------------------------

terraform {
  backend "s3" {
    bucket       = "retrain-pipeline-tfstate-646278323015"
    key          = "terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}
