# Operations

Running the pipeline, reviewing what it proposes, what it costs, and what breaks.

The pipeline proposes. A human disposes. Most of this document is about being that human.

## The reviewer runbook

A model package lands in the registry as `PendingManualApproval` with its metrics and lineage
attached. This is what the reviewer checks before it becomes anything more.

### 1. Find the pending version

```bash
aws sagemaker list-model-packages \
  --model-package-group-name retrain-pipeline-models \
  --model-approval-status PendingManualApproval

aws sagemaker describe-model-package --model-package-name <model-package-arn>
```

### 2. Read the metrics against the incumbent

The decision is a comparison, not an absolute judgment. Fetch
`ModelMetrics.ModelQuality.Statistics.S3Uri` for the candidate and for the current `Approved` version:

```bash
aws sagemaker list-model-packages \
  --model-package-group-name retrain-pipeline-models \
  --model-approval-status Approved            # the incumbent, if there is one
```

On this dataset the two error types cost different things: a precision drop junks real messages, while
a recall drop only lets more spam through. **A candidate that trades precision for recall is usually
the wrong trade here**, and saying so is the reviewer's job. A single headline accuracy figure hides
exactly this, which is why the metrics file carries all four numbers.

**When there is no incumbent**, which is the case for the first version in the group and any time
every prior version was rejected, there is nothing to compare against and the step still has to mean
something. Judge the candidate against the documented local baseline instead (accuracy 0.97, precision
1.00, recall 0.81, F1 0.90) and treat a large divergence in either direction as a reason to look
closer. Numbers far below it suggest the job trained on the wrong data; numbers suspiciously near
perfect suggest holdout leakage. Approving a first version is approving a baseline, so say that in the
description rather than implying a comparison that did not happen.

**Identical metrics are a real outcome, not a bug.** A small batch can shift every coefficient without
flipping a single holdout prediction, producing a model that is provably different and measurably
identical. That is a judgment call rather than an error: promoting it costs nothing but sets a
precedent of approving on process rather than evidence. This has already happened once here; see
the README's `Results`.

### 3. Verify the lineage resolves

`CustomerMetadataProperties` carries `dataset_hash`, `git_sha`, and `training_job_name`. The chain runs
from the registry entry down to bytes, and each hop is checkable on its own:

```bash
git show --stat <git_sha>                      # the commit exists
git merge-base --is-ancestor <git_sha> main    # and it actually merged through the gate
git show <git_sha>:data/train.csv.dvc          # its md5 must equal dataset_hash
git checkout <git_sha> && dvc pull             # the exact bytes that hash names
md5sum data/train.csv                          # must equal dataset_hash again
```

**The ancestry check is the one worth not skipping.** "A real commit" and "a commit that passed review
and merged" are different claims, and only the second means the data cleared the quality gate. A
package whose `git_sha` is not reachable from `main` was trained on data that never passed.

If the hash in the pointer at that commit does not match the metadata, the package is not describing
the data you think it is, and that is a reject regardless of how good the numbers look.

### 4. Approve or reject, with a reason

```bash
aws sagemaker update-model-package \
  --model-package-arn <model-package-arn> \
  --model-approval-status Approved \
  --approval-description "f1 0.90 vs champion 0.88, lineage verified against <git_sha>"
```

The description is the audit trail. An approval with no stated reason is a click, not a decision, and
it should name what was compared: either the incumbent version it beat, or the fact that it is a
baseline with no incumbent.

### Why the human cannot be skipped

The CI role holds `CreateModelPackage`, `DescribeModelPackage`, and `ListModelPackages`, and
deliberately not `UpdateModelPackage`. Approval status is changed only by `UpdateModelPackage`, so CI
can propose a version and read the registry but structurally cannot approve one. **The gate is
enforced by a missing IAM permission, not by documentation.** Promotion requires a principal that CI
is not.

## Cost

| Item | Cost |
|---|---|
| Three S3 buckets | Cents per month at this data volume |
| Two IAM roles, model package group, OIDC provider | Free |
| GitHub Actions on a public repo | Free |
| One training job, `ml.m5.large`, a few minutes | Cents |

**Nothing in this project bills while idle.** There is no endpoint, no NAT gateway, no GPU, and
nothing to tear down between sessions. That is a deliberate design constraint rather than a happy
accident: the training job is capped at 20 minutes by its own `StoppingCondition`, and the instance
type defaults to a small CPU SKU so a bare `trainctl submit` cannot pick a billing surprise.

The one cost worth watching is training jobs launched by accident. The `train` workflow's path filter
on `data/**` is what prevents a README merge from starting one.

## Failure modes

### The `validate` check is pending forever

Someone added a path filter to `quality-gate`. A required check that does not run never reports, and
the PR can never merge. The gate must run on every PR and branch internally on `git diff`. See
[ARCHITECTURE.md](ARCHITECTURE.md).

### `ResourceLimitExceeded` from `CreateTrainingJob`

A SageMaker training quota of zero, which is the default on a new account. This reads like a code bug
and is not one.

```bash
aws service-quotas get-service-quota --service-code sagemaker --quota-code L-611FA074
```

### The training job failed

`trainctl` prints the status, the failure reason, and a console link that carries a native "View logs"
link. Start there:

```bash
aws sagemaker describe-training-job --training-job-name <name> \
  --query '{status:TrainingJobStatus,reason:FailureReason}'
```

The most likely cause is an import error from the sklearn version gap: local pins 1.9, the container
runs the managed 1.4 image, and `requirements.txt` is deliberately not shipped into it. See
[LOCAL_DEV.md](LOCAL_DEV.md).

### `submit` refuses with a duplicate job name

Working as designed. The job name is derived from the dataset hash and the commit, so identical inputs
resolve to a name that already exists and SageMaker rejects it. That is the idempotency guarantee, not
a failure. A genuine re-run needs a new commit.

### `register` says the job is not `Completed`

`register` refuses to register anything but a completed job, so an unservable artifact never reaches a
reviewer. If `submit` went red, fix that first. If `submit` passed and `register` failed on a
permissions detail, re-run just the registration without paying to train again:

```bash
make register
```

### CI cannot assume its role

The trust policy's `sub` condition does not match the token's claims, or the job is missing
`id-token: write` and never got a token. Both produce the same error text. Check the workflow
permissions block first, since it is the cheaper of the two to rule out, then the numeric owner and
repo IDs in `infra/variables.tf`.

## Honest constraints

Things this pipeline does not do, stated plainly rather than left for a reviewer to discover.

- **The trigger is CI-driven, not event-driven.** A merge to `main` starts a workflow. There is no
  queue, no event bus, and no EventBridge. Retraining on a schedule or on a drift signal would be a
  different architecture.
- **There is one dataset and one model.** The pointer paths are fixed in `trainctl`, because this
  pipeline trains one model against one frozen holdout. Multi-dataset support would change the CLI's
  shape, not just its flags.
- **Approval is manual by design and does not scale.** A human reads metrics and lineage for every
  version. That is the point of the project, and it is also the reason this pattern suits models that
  retrain weekly rather than hourly.
- **Nothing deploys the approved model.** Approval marks a version promotable; serving it is
  `inference-gateway`'s job, and the handoff between the two is not automated.
- **The holdout is frozen, so the metrics are comparable but not unbiased forever.** Every model has
  now been measured against the same 1,115 rows. Enough iterations of judging candidates on one test
  set eventually overfits to it, and this project is nowhere near that point but does not pretend the
  ceiling does not exist.
