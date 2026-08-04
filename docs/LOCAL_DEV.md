# Local development

The whole training loop runs on a laptop. Nothing here needs an AWS account except pulling the
dataset, and there is a path around that too.

## Run it

```bash
# 1. Clone
git clone https://github.com/Go-Santiago-Go/retrain-pipeline.git
cd retrain-pipeline

# 2. Create and activate a virtual environment, then install
python -m venv .venv && source .venv/bin/activate
make install

# 3. Fetch the dataset and the frozen holdout from the DVC remote
make data

# 4. Train against the frozen holdout
make train
```

`make train` prints the metrics it just wrote and leaves `model.joblib` and `metrics.json` in
`training/model/`:

```json
{
  "accuracy": 0.9748878923766816,
  "precision": 1.0,
  "recall": 0.8120805369127517,
  "f1": 0.8962962962962963
}
```

`make install` deliberately does not create the virtual environment for you. A Makefile cannot
activate one in the caller's shell, so a target that made it would leave you installing into whatever
interpreter happened to be first on `PATH`.

## If you do not have AWS access

`make data` runs `dvc pull`, which needs read access to this project's S3 remote. Without it, rebuild
the dataset from the public source instead:

```bash
mkdir -p data/raw
curl -sL "https://archive.ics.uci.edu/static/public/228/sms+spam+collection.zip" -o data/raw/smsspam.zip
unzip -o data/raw/smsspam.zip -d data/raw/

python training/split_dataset.py   # carves a stratified 20 percent holdout under seed 42
make train
```

**Understand what this costs you.** The split is deterministic under seed 42, so you will get the same
partition of the original UCI corpus. But the dataset tracked in this repo also contains 24 hand
written messages that entered later through the pipeline itself, and those are not in the public
download. Your metrics will therefore be close to the published baseline without matching it exactly,
and they are not comparable to the numbers in the model registry. That is a fine way to read the code
and a bad way to evaluate a model.

## The frozen holdout, and why there is no `make split`

`training/split_dataset.py` ran **once**. Its output, `data/holdout.csv`, is DVC-tracked like any
other data and is the fixed ruler every model in this project is measured against.

Re-running it in this repo would silently invalidate every metric ever recorded, including the ones
attached to approved model packages, because the models would no longer share a test set. Nothing
would error. The numbers would just quietly stop meaning what they claim.

So the script is a one-shot artifact rather than a workflow, and the Makefile has no target for it.
The deeper guard is structural: `train.py` contains no split logic at all, so a training run cannot
resplit no matter how it is invoked. See [ARCHITECTURE.md](ARCHITECTURE.md) on the two leakage guards.

## Configuration

`train.py` reads two environment variables, both of which SageMaker script mode sets in the container
and neither of which you normally set by hand:

| Variable | Default | Purpose |
|---|---|---|
| `SM_CHANNEL_TRAIN` | `data` | Directory holding `train.csv` and `holdout.csv`. SageMaker points this at the input channel it downloaded. |
| `SM_MODEL_DIR` | `training/model` | Directory for `model.joblib` and `metrics.json`. SageMaker tars whatever lands here into `model.tar.gz`. |

Defaulting both to local paths is what makes the file dual-mode: the identical script runs on a laptop
and in the cloud with no branching. To point a local run at different data, set them:

```bash
SM_CHANNEL_TRAIN=/tmp/experiment SM_MODEL_DIR=/tmp/out python training/train.py
```

AWS credentials come from the standard chain, so `AWS_PROFILE` and `AWS_REGION` work as usual for
`make data` and for the `trainctl` targets. There are no project-specific credential variables.

## Development commands

```bash
make test        # Go test suite: job name derivation, request wiring, S3 URI parsing, tar extraction
make lint        # go vet plus a gofmt check
make build       # compile trainctl
make validate    # run the Great Expectations suite against data/train.csv
make help        # everything else
```

`make test` needs no AWS access and no network. The request builders (`buildTrainingInput`,
`buildModelPackageInput`) are pure functions of a params struct precisely so the wiring can be
asserted in a table test without a client, and `extractFile` takes an `io.Reader` so a test can hand
it an in-memory archive instead of an S3 object.

## Running the quality gate locally

`make validate` runs the same script CI runs, against the same data contract:

```bash
make validate                                  # validates data/train.csv
python training/validate.py path/to/broken.csv # point it at a deliberately bad fixture
```

It exits nonzero on any failed expectation and writes a per-expectation report to
`training/validation-result.json`. That exit code is the entire gate mechanism: in CI the same
nonzero exit fails the step, and the step is a required check, so the merge is blocked.

Running it locally before opening a data PR is the cheap version of the loop. The expensive version
takes about a minute in CI and tells you the same thing.

## A local gotcha

Local development pins scikit-learn 1.9 while the training container runs the AWS-managed sklearn
1.4 image. The TF-IDF and LogisticRegression APIs used here are stable across that gap, which is why
one `train.py` works in both, but **a model trained locally and a model trained in SageMaker are not
byte-identical artifacts**, and unpickling one under the other's version will warn.

This is intentional and `training/requirements.txt` is deliberately not shipped into the container,
so it keeps its own pinned runtime rather than pip-installing a newer sklearn over itself mid-job.
If you add a dependency that only exists in the newer sklearn, the local run will pass and the
SageMaker job will fail at import. The gap is the price of not maintaining a custom training image,
and for a TF-IDF pipeline it is the right trade.
