"""One-shot: carve the frozen holdout from the raw UCI dataset.

Run once. train.py must never resplit; that is why the split lives here
and not in the training runtime.
"""

import csv
from pathlib import Path

import pandas as pd
from sklearn.model_selection import train_test_split

RAW = Path("data/raw/SMSSpamCollection")
TRAIN_OUT = Path("data/train.csv")
HOLDOUT_OUT = Path("data/holdout.csv")
SEED = 42

# Raw file is tab-separated, no header. QUOTE_NONE stops pandas from
# treating the " chars inside messages as CSV field quoting, which
# would silently merge rows.
df = pd.read_csv(
    RAW,
    sep="\t",
    header=None,
    names=["label", "text"],
    quoting=csv.QUOTE_NONE,
    encoding="utf-8",
)

# Stratify on label to preserve the ~87/13 ham/spam ratio in both
# splits. Fixed seed makes the carve reproducible.
train_df, holdout_df = train_test_split(
    df, test_size=0.2, random_state=SEED, stratify=df["label"]
)

train_df.to_csv(TRAIN_OUT, index=False)
holdout_df.to_csv(HOLDOUT_OUT, index=False)

print(f"raw={len(df)}  train={len(train_df)}  holdout={len(holdout_df)}")
print("label balance (holdout):")
print(holdout_df["label"].value_counts(normalize=True).round(3))
