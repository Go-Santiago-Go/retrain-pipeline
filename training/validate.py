"""Validate an incoming data batch against the spam dataset contract.

Runs the same locally and in CI: CI dvc-pulls data/, then invokes this. Exits
nonzero on any failed expectation so the quality gate can block the merge.
"""

import json
import sys
from pathlib import Path

import great_expectations as gx
import pandas as pd

# Default to the training CSV; pass a path to point at a broken fixture.
csv_path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("data/train.csv")
df = pd.read_csv(csv_path)

# The Context is GX's entry point: it owns all config and stores.
# mode="ephemeral" = in memory only, nothing persisted to disk. The
# alternative ("file") writes a great_expectations/ project dir; we don't
# want that state to manage in CI (preflight D2).
context = gx.get_context(mode="ephemeral")

# Now the "where does data come from" chain. Read it top to bottom as
# progressively narrowing: engine -> table -> how to slice it.
batch_def = (
    # data source = the engine/connector. add_pandas means "data arrives as
    # a pandas DataFrame." (Swap for add_postgres later and the suite below
    # is unchanged. That reuse is why GX splits data from contract.)
    context.data_sources.add_pandas("spam")
    # asset = a named dataset within that source. For us, one DataFrame.
    .add_dataframe_asset("batch")
    # batch definition = which rows to validate. whole_dataframe = all of
    # them (no partitioning by date etc). Returns a handle we validate later.
    .add_batch_definition_whole_dataframe("whole")
)

# The suite is the contract: A named bag of Expectations. Each Expectation is
# one assertion about the data. Adding them to the suite doesn't run anything
# yet; it just declares what "valid" means. This allows you to evaluate them as
# a group.
suite = context.suites.add(gx.ExpectationSuite(name="train-contract"))
suite.add_expectation(
    gx.expectations.ExpectTableColumnsToMatchSet(
        column_set=["label", "text"], exact_match=True
    )
)
suite.add_expectation(
    gx.expectations.ExpectColumnValuesToBeInSet(
        column="label", value_set=["ham", "spam"]
    )
)
# to_be_in_set skips nulls, so a missing label needs its own check.
suite.add_expectation(gx.expectations.ExpectColumnValuesToNotBeNull(column="label"))
suite.add_expectation(gx.expectations.ExpectColumnValuesToNotBeNull(column="text"))
suite.add_expectation(
    gx.expectations.ExpectColumnValueLengthsToBeBetween(
        column="text", min_value=1, max_value=1000
    )
)
suite.add_expectation(gx.expectations.ExpectTableRowCountToBeBetween(min_value=100))

# ValidationDefinition binds the contract (suite) to the data (batch_def);
# run() is the first line that actually touches df.
validation_def = context.validation_definitions.add(
    gx.ValidationDefinition(name="train-vd", data=batch_def, suite=suite)
)
result = validation_def.run(batch_parameters={"dataframe": df})

# The result JSON is the quarantine record CI uploads when the gate fails.
Path("training/validation-result.json").write_text(
    json.dumps(result.to_json_dict(), indent=2)
)

print(f"validation success: {result.success}")
# Exit code is the gate: nonzero fails the CI job, which blocks the merge.
sys.exit(0 if result.success else 1)
