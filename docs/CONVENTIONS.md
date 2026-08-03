# Conventions

Rules for changing this repository's documentation and for the claims it makes. Code conventions are
the Go defaults plus `go vet`, and the Python here is small enough to need none; these are the ones a
linter cannot enforce and that have been gotten wrong before.

## Accuracy guards

Claims in the docs must match the code and the measurements.

**This pipeline is CI-driven, never event-driven.** The trigger is a merge to `main` that starts a
GitHub Actions workflow. There is no queue, no event bus, and no EventBridge anywhere in this design.
"Event-driven" describes a different architecture with different failure modes and different
interview questions, and every sentence in these docs has to keep the distinction. The accurate
phrasing is *continuous training*: data and code both enter through Git, validation gates the merge,
the merge triggers training, and the registry gates promotion.

**Identical metrics are a real outcome, not a defect.** The registered version 1 scored the same as
the previous model on every holdout metric. The honest reading is that 24 new rows against 4,459
existing ones moved every coefficient without flipping any of the 1,115 holdout predictions, which is
a model that is provably different and measurably identical. Do not write this up as a bug, a
regression, or an improvement. **The supporting figures are paired with that specific run**:
vocabulary 7,714 to 7,726 terms, intercept -2.46755088 to -2.47247791. Re-running training on
different data invalidates all four numbers together.

**No metric appears anywhere without instrumentation behind it.** The baseline (accuracy 0.97,
precision 1.00, recall 0.81, F1 0.90) is the output of `make train` against the frozen holdout, and it
lives in `training/model/metrics.json`. A number that cannot be reproduced by a command in this repo
does not go in the README, the docs, or a resume bullet.

**Precision and recall are never collapsed into accuracy.** On a roughly 87/13 ham-to-spam split,
accuracy hides the failure that matters: a false positive junks a real message, while a false negative
only lets one spam through. Any table quoting a single headline number for this model is misleading,
and the two error types must stay separately visible with their asymmetric cost stated.

**The holdout is frozen and nothing may imply otherwise.** `training/split_dataset.py` ran once under
seed 42 to carve a stratified 20 percent holdout. The split lives in that one-shot script rather than
in `train.py` precisely so a training run structurally cannot resplit. Do not document a workflow that
regenerates it, and do not add a Makefile target that invites it.

**"Demonstrated end to end" is the claim; a running system is not.** The full loop has executed
against real AWS: gate, merge, training job `retrain-pipeline-ce0d529b-c36d27a` to `Completed`,
registration as version 1, human approval with a stated reason. Nothing in that sequence was
simulated. But this repo is a pipeline, not a service, so there is nothing to keep running and no URL
to publish. Present-tense "the pipeline trains models" is the wrong tense for the demonstration; it
has run and can run again.

**CI structurally cannot approve a model, and the reason is an IAM grant rather than a convention.**
The CI role holds `CreateModelPackage`, `DescribeModelPackage`, and `ListModelPackages`, and
deliberately not `UpdateModelPackage`. Any rewrite that describes the human gate as policy or process
has weakened the claim: it is enforced by the absence of a permission, and that is what makes it worth
writing down. If the grant in `infra/iam_ci.tf` ever changes, this sentence is false.

## Generated artifacts

`docs/demo.gif` is **generated, not recorded by hand.** A script runs the real gate against the real
dataset, pulls the rejected batch back out of S3, and reads the training job and model package that
already exist, so every value visible in it is that run's own output.

- Never hand edit it.
- Never describe it with numbers it does not show. The dataset hash, the failing expectation, the one
  corrupted label out of 4,460 rows, the job name, and the approval status in the README's alt text
  **and in the paragraphs under it** come from the run that produced the current file.
- The paragraphs under the gif carry a job the sibling repos' captions do not: establishing that
  almost every command shown is a CI step rather than a developer workflow. Do not let them drift into
  explaining how the pipeline is built; that belongs in [ARCHITECTURE.md](ARCHITECTURE.md).
- Recording it costs nothing and has to stay that way. The gate runs locally and both AWS calls are
  read-only. A live `trainctl submit` capture is not an option: the derived job name means identical
  inputs never train twice, so it could never be re-run.
- A change to `validate.py`'s output or to `trainctl`'s printed output makes it stale, and nothing in
  the tree will say so.
- The generator lives in `content/demo-recording/`, which is gitignored. If it is unavailable, flag the
  gif as stale rather than editing the prose to match a recording the image no longer shows.

## Documentation layout

The docs are split by audience. **`README.md` is the overview and stays short.** Depth lives here:

| File | Scope |
|---|---|
| `docs/ARCHITECTURE.md` | The data path and the training path, the idempotency mechanism, the leakage guards, and where the governance boundary sits |
| `docs/CLI.md` | `trainctl` reference: both subcommands, every flag, the values each derives rather than takes, and exit codes |
| `docs/LOCAL_DEV.md` | Local run from clone to trained model, the environment variables `train.py` reads, development commands, and the frozen holdout |
| `docs/DEPLOYMENT.md` | What gets provisioned on AWS, the Terraform stack, the OIDC trust wiring, branch protection, and teardown |
| `docs/OPERATIONS.md` | The reviewer runbook, cost, the failure modes and the command that diagnoses each, and honest constraints |
| `docs/CONVENTIONS.md` | This file |

**`docs/CLI.md` occupies the slot the sibling repos give to `docs/API.md`.** `rag-api` and
`inference-gateway` are services whose interface is HTTP; this repo's interface is a command line
binary, so the reference doc covers subcommands and flags rather than routes and status codes. The
slot's job is the same in all three: the complete surface a caller can touch.

**Architecture is the pipeline; deployment is the cloud.** `ARCHITECTURE.md` covers the two paths and
all seven design ideas. What Terraform provisions is not architecture in this split: it belongs to
`DEPLOYMENT.md`.

**The README carries five of those seven ideas, and every lead-in must resolve to a heading here.**
The README's `How it works` is an overview, so it names only the load-bearing five and links out; the
first four match `ARCHITECTURE.md` headings by name, and the fifth, *CI can propose a model and cannot
promote one*, deliberately merges this file's `Governance lives in the CLI` and `Two IAM roles`
sections because a reader of the README needs the governance claim as one idea rather than two. The
dual-mode training script and the metrics-key decision live here only. **Adding a sixth lead-in to the
README is how this section grows back into the duplicate it used to be**: the test is whether a
reviewer who stops reading after `How it works` would be missing something they could not defend, and
for those two the answer is no.

**The pipeline diagram is duplicated on purpose, in exactly two places:** `README.md` under
`How it works`, and `ARCHITECTURE.md` at the top. The README needs it because a reviewer who reads
only that file should still see the shape; `ARCHITECTURE.md` needs it because a reference doc must
stand alone. **Edit both or neither.** The copies are byte-identical, so a diff of the two mermaid
blocks is the check. Do not add a third copy.

**When a README section grows past a few paragraphs, move it into the matching file above and leave a
link.** Do not let the README reabsorb depth. The reviewer runbook and the design Q&A both lived in
the README once and both belong in `docs/`.

## The README spine

`README.md` follows a fixed section order, shared across the portfolio repos so they read as one body
of work. Do not rename or reorder these, and do not insert new top-level sections between them:

```
title + badges + what it is → Contents → Demo → The problem → How it works → Quickstart
→ Trade-offs → Results → What I'd do differently → Known gaps and next steps
→ Repo layout → Documentation → License
```

The narrative arc under those names is **problem → approach → trade-offs → results → hindsight**. A
section that does not advance that arc belongs in `docs/`.

Three rules specific to the spine:

- **`Results` ships only if it contains a number a reader could reproduce with a command.** Here that
  command is `make train`. Timings observed in CI are labelled as observations of a named run, not as
  benchmarks, because a GitHub Actions runner is not a controlled environment.
- **`What I'd do differently` is hindsight; `Known gaps and next steps` is scope.** They are different
  claims and must not be merged. Folding a deliberate scoping call into the hindsight section turns a
  defensible decision into an apparent regret, and the reverse hides a real mistake behind "out of
  scope."
- **`Repo layout` is a table, not an ASCII tree.** The tree wastes horizontal space on box drawing and
  cannot hold a full sentence per entry. It ends at the table, with no trailing prose: pointers to the
  files worth reading first belong at the top of `ARCHITECTURE.md`, next to the explanation they point
  at.
- **`Trade-offs` is a four column table: `Decision`, `Choice`, `Why`, `Also considered`.** `Decision`
  names the *concern* rather than the answer, so the column reads as a list of questions a reviewer
  could ask. A `Why` cell is one sentence; if the reasoning needs a paragraph it belongs in `docs/`
  with the cell pointing there. The `Also considered` column is not optional, because a decision with
  no stated alternative has not been shown to be a decision.
- **Section shape is shared across the portfolio repos, not just section names.** `The problem` ends
  in a bulleted list of requirements, each naming what it costs to get wrong. `How it works` opens the
  diagram, then a fixed number of bolded lead-ins, then the interface table, then the deployed shape.
  `Trade-offs` closes with the pattern under the decisions and a one sentence stack summary. `Results`
  leads with a measured table and a command that reproduces it. `What I'd do differently` opens by
  stating how many things and that they are hindsight rather than parked work. **`Known gaps and next
  steps` is bolded lead-in paragraphs, never a bullet list**, and closes with a single `Also parked:`
  sentence sweeping up the items too small for their own paragraph. Matching only the headings while
  filling them with a different structure is the failure mode this rule exists to catch.
- **Tables whose first column is a label, not a category, take an empty header row (`| | |`).** The
  `Contents` table and the `Results` measured table both do this, because a `Metric | Value` header
  states the obvious and renders the first metric as a heading.

## Writing rules

- **Never link the README or `docs/` to anything in `content/`.** That directory is gitignored, so
  those links 404 for anyone reading the repo on GitHub. `content/` holds unpublished drafts only and
  is never staged or committed.
- **Diagrams are single-direction with no back edges.** Mermaid renders as spaghetti once an edge
  points backwards. If a diagram needs to show two concerns, make it two diagrams.
- **No `classDef`, `style`, or `linkStyle` blocks in any diagram, without exception.** Mermaid on
  GitHub inherits the reader's light or dark theme; hardcoded fills do not, so a palette tuned on a
  white background renders as glaring white boxes for anyone reading in dark mode. Meaning goes in the
  shape and the edge, not the color.
- **A diagram's edges carry what crosses them.** In the pipeline diagram the interesting labels are
  the artifacts that move between stages: a pointer, a hash, a tarball, an approval status. An
  unlabelled arrow between two boxes has said only that they are adjacent.
- **The name is `retrain-pipeline` everywhere**: repo, Go module path, README title, doc titles, and
  the AWS resource prefix. Unlike the sibling repos there is no legacy name to avoid, and the resource
  names already match, so the exception `go-rag-api` needs does not apply here.
- **Badges point at workflows that exist.** This repo has `quality-gate` and `train`, and it has no
  `ci` or `deploy` workflow, so copying the sibling repos' badge block verbatim would render two
  permanently broken badges.
- **Verify fast-moving SDK and service shapes against live documentation** rather than memory, then
  write what you verified. Great Expectations 1.x rewrote the API this project uses, and the AWS
  managed sklearn image URIs and SageMaker Model Registry request shapes have both moved under this
  project.
