# CLAUDE.md

How to get this repository running and how to verify a change. Everything else lives in `docs/`; see
the map at the bottom.

`retrain-pipeline` is a CI-driven MLOps pipeline for a spam classifier, where data quality is a merge
requirement and model promotion is a human decision. New labeled data enters only by pull request
carrying a DVC pointer, Great Expectations runs as a required check and blocks the merge if the batch
violates the contract, and merging derives a SageMaker training job name from the dataset hash plus
the commit SHA. A Go CLI (`trainctl`) submits that job and registers the result in the SageMaker Model
Registry as `PendingManualApproval`, where a human approves it against holdout metrics. This is
continuous training triggered by CI. There is no queue, no event bus, and nothing event-driven in it.

**This is a pipeline, not a service.** Nothing here serves traffic, so there is no `make run` and no
`make up`. The local loop is train and validate.

## Run it

The whole training loop runs on a laptop. Only `make data` reaches AWS, and only to read.

```bash
python -m venv .venv && source .venv/bin/activate   # make install will not do this for you
make install                                        # Python deps plus dvc[s3]
make data                                           # dvc pull: dataset and frozen holdout
make train                                          # writes training/model/{model.joblib,metrics.json}
```

`make train` prints the metrics it just wrote:

```json
{
  "accuracy": 0.9748878923766816,
  "precision": 1.0,
  "recall": 0.8120805369127517,
  "f1": 0.8962962962962963
}
```

Without read access to this project's S3 remote, `make data` fails. Rebuild the dataset from the
public UCI source instead; the recipe is in [docs/LOCAL_DEV.md](docs/LOCAL_DEV.md). Your metrics will
land close to the published baseline without matching it, because the tracked dataset also holds 24
hand written messages that entered later through the pipeline and are not in the public download.

`make help` lists every target. The verbs (`help`, `build`, `test`, `lint`, `deploy`, `destroy`) are
the same in every repo in this portfolio.

## Verify a change

What should pass before any commit:

```bash
make build lint test    # compile trainctl, go vet plus gofmt check, Go test suite
make validate           # the Great Expectations suite, the same script CI runs
```

`make test` needs no AWS access and no network. The request builders (`buildTrainingInput`,
`buildModelPackageInput`) are pure functions of a params struct so a table test can assert the wiring
without a client, and `extractFile` takes an `io.Reader` so a test can hand it an in-memory archive.

**No CI workflow runs the Go tests.** There is no `ci.yml` in this repository, so `make test` and
`make lint` are a habit rather than a control. That is a known gap, named in the README, and it means
running them locally is not optional.

The two AWS targets bill and are not part of the normal loop:

```bash
make submit      # CreateTrainingJob and poll to a terminal state. Costs cents.
make register    # register the completed artifact as PendingManualApproval
```

Normally the `train` workflow runs both on merge to `main`. They exist as targets for re-running a
registration by hand after a permissions failure, without paying to train again.

## Things that will waste your time

- **Activate a virtual environment first.** `make install` deliberately does not create one, because a
  Makefile cannot activate a venv in the caller's shell and a target that made one would leave you
  installing into whatever interpreter is first on `PATH`. Symptom of forgetting: `make validate`
  dies with `make: python: Not a directory`.
- **Never run `training/split_dataset.py` in this repo.** It ran once. `data/holdout.csv` is the fixed
  ruler every recorded metric was measured against, including metrics attached to approved model
  packages. Re-running it errors nothing and silently invalidates all of them. This is why there is no
  `make split`.
- **`ResourceLimitExceeded` from `CreateTrainingJob` is a quota, not a code bug.** A new AWS account
  has a SageMaker training instance quota of zero. Check it before debugging anything else:
  `aws service-quotas get-service-quota --service-code sagemaker --quota-code L-611FA074`.
- **`submit` refusing a duplicate job name is correct behavior.** The name is a deterministic function
  of the dataset hash and the commit, and SageMaker rejecting a repeat is the idempotency mechanism.
  Do not replace it with a check-then-act existence test, which would add a time-of-check-to-time-of-use
  race that this design does not have.
- **Never add a path filter to `quality-gate`.** It runs on every PR and branches internally on `git
  diff`. A required check that does not run never reports, so a code-only PR would sit unmergeable
  forever. The asymmetry with `train`, which does filter on `data/**`, is deliberate: nothing blocks
  on a post-merge job, and that job bills.
- **CI never commits and never promotes.** `dvc add` and `dvc push` are run by a human locally; CI only
  ever runs `dvc pull`. The CI role holds `CreateModelPackage` and deliberately not
  `UpdateModelPackage`, so the approval gate is an absent IAM permission rather than a written policy.
  Granting it would remove the control this project exists to demonstrate.
- **Local scikit-learn is 1.9, the training container is the AWS-managed 1.4 image.** The TF-IDF and
  LogisticRegression APIs used here are stable across that gap, which is why one `train.py` runs in
  both. A dependency that only exists in the newer sklearn will pass locally and fail at import inside
  the SageMaker job.
- **Nothing bills while idle.** No endpoint, no NAT gateway, no GPU. `make destroy` exists for teardown,
  not for cost, and there is no billable stack to take down between sessions. The one real cost risk is
  a training job launched by accident, which the `train` workflow's `data/**` path filter is what
  prevents.
- **Before writing docs, read [docs/CONVENTIONS.md](docs/CONVENTIONS.md).** It carries the accuracy
  guards, and each one is there because it was gotten wrong. No claim gets a number that was not
  measured in this repo.

## Where everything is

| Doc | Scope |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | The two paths and why they trigger differently, the hash as identity, the idempotency mechanism, both leakage guards, the governance boundary |
| [docs/CLI.md](docs/CLI.md) | `trainctl` reference: both subcommands, every flag, the values each derives rather than takes, exit codes |
| [docs/LOCAL_DEV.md](docs/LOCAL_DEV.md) | Clone to a trained model, the no-AWS path, the environment variables `train.py` reads, why there is no `make split`, the sklearn version gap |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | What Terraform provisions, the SageMaker quota that blocks everything, the state migration, branch protection, teardown |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | The reviewer runbook for approving a model, cost, every failure mode and the command that diagnoses it |
| [docs/CONVENTIONS.md](docs/CONVENTIONS.md) | Documentation rules, the README spine, accuracy guards, generated artifacts |

| Path | Contents |
|---|---|
| `cmd/trainctl/` | The Go CLI. `submit` and `register`; `name.go` derives the idempotent job name. Request builders are pure functions so table tests can assert wiring without AWS. |
| `training/train.py` | Dual-mode training. Reads `SM_CHANNEL_TRAIN` and `SM_MODEL_DIR`, defaults them to local paths, writes `metrics.json` beside the model so scores travel inside the artifact. |
| `training/validate.py` | The Great Expectations suite. Exits nonzero on any failed expectation, which is the entire gate mechanism. |
| `training/split_dataset.py` | The one-shot holdout carve under seed 42. Ran once, must never run again. |
| `data/` | DVC pointer files only, never the data itself. |
| `terraform/` | Three buckets, two IAM roles, the model package group, and a data source reading the account-global OIDC provider it deliberately does not own. |
| `.github/workflows/` | `quality-gate` runs on every PR and branches on `git diff`; `train` runs on merges touching `data/**`. The asymmetry is the most counterintuitive decision in the repo. |
| `Makefile` | Task runner. `make help` lists the targets. |
