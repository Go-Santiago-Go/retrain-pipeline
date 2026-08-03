# `trainctl` reference

The Go CLI that submits training jobs and registers their results. Two subcommands, both run by the
`train` workflow on merge to `main`, and both runnable by hand against a completed job.

```
usage: trainctl <submit|register>
```

`trainctl` takes only the identifiers it cannot know: role ARNs, an image URI, a bucket name, a
registry group. Everything else is derived from the committed `.dvc` pointer and the current commit,
so CI has nothing to restate and nothing to get wrong. Both subcommands resolve the same job name
from the same two inputs independently, which is why no job name is ever passed between workflow
steps.

Build it with `make build`, or run it through `go run ./cmd/trainctl`.

## Credentials

`config.LoadDefaultConfig` walks the standard AWS credential chain, so the same binary authenticates
from SSO on a laptop and from the OIDC web-identity role in CI, with no flag and no branch. Nothing
long-lived is stored anywhere. See [DEPLOYMENT.md](DEPLOYMENT.md) for the trust policy.

## `trainctl submit`

Creates a SageMaker training job and blocks until it reaches a terminal state.

```bash
trainctl submit \
  --execution-role arn:aws:iam::<account>:role/retrain-pipeline-sagemaker-execution \
  --image 683313688378.dkr.ecr.us-east-1.amazonaws.com/sagemaker-scikit-learn:1.4-2-cpu-py3 \
  --bucket retrain-pipeline-model-artifacts-<account>
```

| Flag | Default | Purpose |
|---|---|---|
| `--execution-role` | none, required | Role ARN the training job assumes. Distinct from the CI role `trainctl` itself runs under. |
| `--image` | none, required | Managed sklearn framework image URI. Script mode injects `train.py` into it, so no custom image or ECR repository is needed. |
| `--bucket` | none, required | Model-artifacts bucket name, no scheme. The `code/`, `input/`, and `output/` prefixes derive from it. |
| `--instance-type` | `ml.m5.large` | CPU instance type. Defaults small so a bare submit cannot pick a billing surprise, and no GPU type belongs here. |

The S3 paths are not independent inputs. They are one bucket plus a fixed layout the CLI owns, which
is why CI wires up a single `--bucket`:

| Derived path | Value |
|---|---|
| Source code | `s3://<bucket>/code/sourcedir.tar.gz` |
| Input channel | `s3://<bucket>/input/<full dataset md5>/` |
| Output prefix | `s3://<bucket>/output/` |

Folding the dataset hash into the input prefix is the payoff: the bucket value stays stable across
dataset versions, the prefix is immutable per version, and nobody hand-edits a path when the data
changes.

**Derived values.** The dataset md5 comes from `data/train.csv.dvc`, the holdout md5 from
`data/holdout.csv.dvc`, and the commit from `git rev-parse`. The job name is
`retrain-pipeline-<hash8>-<sha7>`; see [ARCHITECTURE.md](ARCHITECTURE.md) for why that name is the
idempotency mechanism.

**Lineage is carried twice, on purpose.** `dataset_hash`, `git_sha`, and `holdout_hash` go in as both
SageMaker tags and hyperparameters. Tags answer "which job trained on this data" from the AWS side
without opening the job; hyperparameters answer "what did this job train on" from inside the job's own
record.

**Timeouts.** Submission gets 30 seconds, since it is a fast control-plane call that returns when the
job is accepted rather than when training finishes. The poll loop gets 30 minutes and ticks every 15
seconds. The job's own `StoppingCondition` caps it at 20 minutes server-side, so the client deadline
stays deliberately looser than the server's: SageMaker owns the authoritative stop, and the client
never abandons a still-billing job.

**Output.**

```
submitted: arn:aws:sagemaker:us-east-1:<account>:training-job/retrain-pipeline-ce0d529b-c36d27a
completed: retrain-pipeline-ce0d529b-c36d27a
```

On a non-`Completed` terminal state it prints the status, the `FailureReason`, and a link to the job's
console page, which carries a native "View logs" link.

## `trainctl register`

Turns a completed training job into a versioned model package awaiting human approval.

```bash
trainctl register \
  --group retrain-pipeline-models \
  --image 683313688378.dkr.ecr.us-east-1.amazonaws.com/sagemaker-scikit-learn:1.4-2-cpu-py3
```

| Flag | Default | Purpose |
|---|---|---|
| `--group` | none, required | Model package group the version lands in. SageMaker assigns the next version number within it. |
| `--image` | none, required | Container that can serve the artifact. The managed sklearn image ships both training and serving entry points, so it is the same URI `submit` trains with. |

No `--bucket`: `register` asks `DescribeTrainingJob` where the artifact landed and publishes metrics
beside it, so there is no second flag that has to agree with the first.

**What it does, in order.**

1. Derives the same job name `submit` did, from the same pointer and commit.
2. `DescribeTrainingJob`, and **refuses to proceed unless the status is `Completed`**. Registering a
   failed job would put an unservable artifact in front of a reviewer.
3. Streams `model.tar.gz` out of S3 and extracts `metrics.json` from it without buffering the
   compressed bytes or writing a temp file.
4. Prints the candidate's metrics, so the numbers land in the CI log next to the run that produced
   them.
5. Republishes the metrics as `metrics/<job name>.json` in the same bucket, because `ModelMetrics`
   takes an S3 URI rather than numbers.
6. `CreateModelPackage` with status `PendingManualApproval`.

**Metadata on the package.** `CustomerMetadataProperties` carries `dataset_hash`, `git_sha`, and
`training_job_name`, which together answer "which data, which commit, which run" from the registry
entry alone.

Tags are deliberately absent here. `CreateModelPackage` with tags requires `sagemaker:AddTags`, and
the CI role grants that only on training-job ARNs, so tagging would fail at runtime. Lineage rides in
the metadata properties instead, which `CreateModelPackage` covers on its own.

**Output.**

```
candidate metrics: {"accuracy": 0.97..., "precision": 1.0, "recall": 0.81..., "f1": 0.89...}
metrics published: s3://retrain-pipeline-model-artifacts-<account>/metrics/retrain-pipeline-ce0d529b-c36d27a.json
registered (PendingManualApproval): arn:aws:sagemaker:...:model-package/retrain-pipeline-models/1
```

The ARN ends in the version number SageMaker assigned, so that line is what tells a reviewer which
version to open. It is the handoff from the automated half of the pipeline to the human half.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. For `submit`, the training job reached `Completed`. |
| `1` | A runtime failure: AWS call failed, job ended `Failed` or `Stopped`, poll deadline expired, or the job was not `Completed` at registration. |
| `2` | Usage error: no subcommand, an unknown subcommand, or an unparseable flag. |

The distinction that matters for CI is that a *failed training job* and a *failed watch* both exit
`1` but mean different things, and the message says which. A failed job is a red check about the
model; a failed watch is a red check about the pipeline.

Policy lives with the caller rather than the poller: `pollTrainingJob` only watches to a terminal
state and returns the full `Describe` output, and `runSubmit` decides that anything but `Completed`
is a failure. That split is what makes the poller reusable and the policy readable in one place.

## Why registration is a separate step from submission

In the `train` workflow these are two steps rather than one flag on `submit`. GitHub Actions only runs
the second if the first exited zero, so a failed training job can never register a model, and the two
concerns stay separately readable in the log. It also means a registration that fails on a
permissions detail can be re-run by hand against the already completed job without paying to train
again:

```bash
make register
```
