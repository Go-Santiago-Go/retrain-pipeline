# Deploying retrain-pipeline to AWS

What the Terraform stack creates, how to apply it into a fresh account, and how to tear it down.

Unlike the sibling repos in this portfolio there is no billable app stack here and no teardown ritual
after each session. **Every resource in this configuration is free at rest**: three S3 buckets holding
a few megabytes, two IAM roles, and an empty model package group. The only thing that ever bills is a
training job, which runs for a few minutes and stops.

## What gets provisioned

| Resource | Purpose | Idle cost |
|---|---|---|
| `retrain-pipeline-tfstate-<account>` | Terraform remote state. Versioned, with noncurrent versions expiring after 7 days. | Cents per month |
| `retrain-pipeline-dvc-remote-<account>` | Content-addressed dataset blobs, pushed by a human and pulled by CI. | Cents per month |
| `retrain-pipeline-model-artifacts-<account>` | `sourcedir.tar.gz`, staged CSVs, `model.tar.gz`, republished metrics. | Cents per month |
| `retrain-pipeline-ci` (IAM role) | Assumed by GitHub Actions via OIDC. Starts training jobs, never runs them. | Free |
| `retrain-pipeline-sagemaker-execution` (IAM role) | Assumed by the training job. Reads input, writes artifacts and logs. | Free |
| `retrain-pipeline-models` | Model package group. The registry container versions land in. | Free |
| GitHub OIDC provider | **Referenced, not created.** See below. | Free |

All three buckets block public access and use SSE-S3 encryption, and all three abort incomplete
multipart uploads after one day. Only the state bucket is versioned: the other two are write-once by
content hash, so versioning would pay to store duplicates of immutable objects.

**There is no DynamoDB lock table.** The S3 backend uses `use_lockfile = true` for native locking,
which has been the supported approach since Terraform 1.10 and removes a resource that used to be
mandatory boilerplate.

**The OIDC provider is account-global and owned elsewhere.** Exactly one identity provider per
account is allowed per URL, and all three portfolio repos authenticate GitHub Actions through it. A
sibling project creates it; this configuration reads it with a data source and only needs its ARN for
the trust policy. Owning it here would mean this repo's `terraform destroy` breaking CI in the other
two.

## Prerequisites

- Terraform 1.10 or newer, for `use_lockfile`
- AWS credentials with permission to create S3 buckets, IAM roles, and SageMaker resources
- Go 1.26 and Python 3.12
- `dvc[s3]` 3.67.1, pinned so local and CI resolve the same bytes the same way
- **A SageMaker training quota above zero.** This is the one that will surprise you; see below.

### The quota that blocks everything

A new AWS account has a service quota of **zero** for SageMaker training instances. Not a low number,
zero. Every `CreateTrainingJob` fails with `ResourceLimitExceeded`, which reads like a code bug and
is not one.

Request an increase before the first run:

```bash
# ml.m5.large for training job usage
aws service-quotas request-service-quota-increase \
  --service-code sagemaker \
  --quota-code L-611FA074 \
  --desired-value 20
```

Approval is not instant. Budget a day or two. If `trainctl submit` returns `ResourceLimitExceeded`,
check the quota before debugging anything else.

## Step 1: Clone

```bash
git clone https://github.com/Go-Santiago-Go/retrain-pipeline.git
cd retrain-pipeline
```

## Step 2: Point the configuration at your account

Three things are hardcoded to the original account and repository and must change for a fresh deploy.

**The backend bucket** in `infra/backend.tf`. Backend configuration is read before variables are
evaluated, so it cannot be interpolated and the account number is literal:

```hcl
bucket = "retrain-pipeline-tfstate-<your account>"
```

**The GitHub identity** in `infra/variables.tf`. These are interpolated into the CI role's trust
policy and are what stop any other repository on GitHub from assuming it. Repositories created after
2026-07-15 use immutable subject claims carrying numeric IDs, because a released name can be
re-registered by someone else while an ID cannot:

```bash
gh api repos/OWNER/REPO --jq .owner.id   # github_owner_id
gh api repos/OWNER/REPO --jq .id         # github_repo_id
```

**The role ARNs and bucket names** in `.github/workflows/quality-gate.yml` and
`.github/workflows/train.yml`. These are inline rather than secrets on purpose: they are public
names, not credentials, and belong in version control where a reviewer can see them.

## Step 3: Apply, then migrate state

There is a chicken-and-egg problem here: the configuration creates the bucket that holds its own
state. So the first apply runs with local state, and the backend is migrated in afterwards.

```bash
# Comment out the backend block in infra/backend.tf first
make deploy

# Uncomment it, then migrate the local state file into the bucket it just created
terraform -chdir=infra init -migrate-state
```

Terraform will ask to copy the existing state; answer yes. Subsequent applies are just `make deploy`.

Confirm the outputs the workflows and `trainctl` consume:

```bash
make outputs
```

## Step 4: Configure the DVC remote

```bash
dvc remote add -d s3remote s3://retrain-pipeline-dvc-remote-<account>/dvc
dvc push
```

`dvc push` uploads the CSV bytes the committed pointers name. Until it runs, CI can resolve a pointer
but not fetch what it points at, and the gate fails at `dvc pull` rather than at validation.

## Step 5: Make the gate a required check

The quality gate blocks nothing until branch protection says it does. This is the step that turns the
project from a demonstration into a control.

On `main`, require the `validate` status check to pass before merging, and enable the setting that
applies the rule to administrators too. Without admin enforcement the gate is advisory for exactly the
person most likely to be merging.

Verify it by opening a PR with a deliberately broken batch. The check goes red, the merge button is
disabled, and the per-expectation report is attached to the run as the `gx-validation-result`
artifact.

## Step 6: Tear it down

Teardown is not a cost measure here, since nothing bills at rest. It matters when you are done with
the project or rebuilding it.

**Migrate state back to local first.** `destroy` would otherwise delete the bucket holding the state
it is currently using, and lose track of everything else mid-operation:

```bash
# Comment out the backend block, then pull state back down
terraform -chdir=infra init -migrate-state

make destroy
```

Two things `destroy` will not remove, both deliberately:

- **The GitHub OIDC provider**, because this configuration only references it. Deleting it breaks the
  sibling repos.
- **Non-empty buckets.** S3 refuses to delete a bucket with objects in it, and the DVC remote will
  have dataset blobs. Empty them first if you mean it, and understand that this discards every
  dataset version the registry's lineage points at.

## Troubleshooting

**`ResourceLimitExceeded` on `CreateTrainingJob`.** The quota above. Not a code problem.

**`Error: creating IAM OIDC Provider: EntityAlreadyExists`.** Something is trying to create the
provider rather than read it. This configuration uses a data source; check that no sibling repo's
state also claims ownership.

**CI cannot assume its role, `Not authorized to perform sts:AssumeRoleWithWebIdentity`.** The trust
policy's `sub` condition does not match the token. Confirm `github_owner_id` and `github_repo_id`
against `gh api`, and confirm the workflow declares `id-token: write`. A job without that permission
never gets a token at all, and the error looks identical.

**`dvc pull` fails in CI with access denied.** The CI role grants `s3:GetObject`, `s3:PutObject`,
`s3:DeleteObject` on the DVC remote's objects and `s3:ListBucket` on the bucket itself. All four are
needed; DVC lists before it fetches.

**`CreateModelPackage` denied.** The model package group name must match the ARN pattern in the CI
role's policy, which is scoped to `retrain-pipeline*`. Renaming the group in `sagemaker.tf` without
updating `iam_ci.tf` fails only at registration time, after training has already been paid for.

**The `validate` check never reports on a code-only PR.** It should, since `quality-gate` has no path
filter by design. If someone adds one, every code-only PR becomes permanently unmergeable. See
[ARCHITECTURE.md](ARCHITECTURE.md).
