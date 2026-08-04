# Task runner. The verbs (help, build, test, lint, deploy, destroy) are the same
# in every repo in this portfolio, so a reviewer who has seen one knows what to
# type here regardless of the language underneath. This repo is a pipeline rather
# than a service, so there is no `run` and no `up`/`down`: nothing here serves
# traffic, and the local loop is train and validate instead.

# Load a local .env if present, so targets pick up AWS_REGION or AWS_PROFILE
# without the caller having to export anything. `-include` keeps this optional.
-include .env
export

# The AWS-published sklearn image. Pinned to 1.4 to match the training container
# CI submits with; see docs/CONVENTIONS.md on why the local pin is newer.
SKLEARN_IMAGE := 683313688378.dkr.ecr.us-east-1.amazonaws.com/sagemaker-scikit-learn:1.4-2-cpu-py3

TF := terraform -chdir=infra

.DEFAULT_GOAL := help

.PHONY: help install data train validate build test lint \
        deploy outputs destroy submit register

help: ## List the available targets
	@grep -hE '^[a-z][a-zA-Z0-9_-]*:.*?## ' $(MAKEFILE_LIST) \
	  | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# --- Local -------------------------------------------------------------------
# The whole training loop runs on a laptop with no AWS account. Only `data`
# reaches the cloud, and only to read.

install: ## Install the Python dependencies into the active environment
	pip install -r training/requirements.txt
	pip install 'dvc[s3]==3.67.1'

data: ## Pull the dataset and frozen holdout from the DVC remote (needs AWS read access)
	dvc pull data/train.csv.dvc data/holdout.csv.dvc

train: ## Train locally against the frozen holdout, writing model.joblib and metrics.json
	python training/train.py

validate: ## Run the Great Expectations suite, the same gate CI enforces on data PRs
	python training/validate.py data/train.csv

# There is deliberately no `split` target. training/split_dataset.py carved the
# frozen holdout once and must never run again: re-splitting silently changes the
# ruler every past metric was measured against. See docs/LOCAL_DEV.md.

build: ## Compile the trainctl CLI
	go build ./...

test: ## Run the Go test suite (no AWS access needed)
	go test ./...

lint: ## Vet and formatting check
	go vet ./...
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
	  echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

# --- Cloud -------------------------------------------------------------------
# One stack, not two. Unlike the sibling repos there is no billable app stack to
# tear down after a session: every resource here (buckets, IAM roles, the model
# package group) is free at rest. `destroy` exists for teardown, not for cost.

deploy: ## Apply the Terraform stack (buckets, IAM roles, model package group)
	$(TF) init
	$(TF) apply

outputs: ## Print the stack outputs trainctl and CI consume
	@$(TF) output

destroy: ## Tear the stack down. Not needed for cost; nothing here bills at rest.
	$(TF) destroy

# --- Training jobs -----------------------------------------------------------
# These bill. A training job on ml.m5.large costs cents and runs for minutes, but
# it is the only thing in this repo that costs anything at all, so it stays behind
# an explicit target rather than riding along with `deploy`.
#
# Normally CI runs both of these on merge to main. They are here for re-running a
# registration by hand after a permissions failure, without paying to train again.

submit: ## Submit a SageMaker training job and watch it to completion (BILLS)
	@go run ./cmd/trainctl submit \
	  --execution-role "$$($(TF) output -raw sagemaker_execution_role_arn)" \
	  --image "$(SKLEARN_IMAGE)" \
	  --bucket "$$($(TF) output -raw model_artifacts_bucket)"

register: ## Register the completed job's artifact as PendingManualApproval
	@go run ./cmd/trainctl register \
	  --group "$$($(TF) output -raw model_package_group_name)" \
	  --image "$(SKLEARN_IMAGE)"
