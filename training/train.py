"""Train the SMS spam classifier.

Dual-mode: SageMaker script mode sets SM_* env vars pointing at container
paths; those default to local paths here so the identical file runs on a
laptop and in the training container with no branching.
"""

import json
import os
from pathlib import Path

import joblib
import pandas as pd
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import accuracy_score, precision_score, recall_score, f1_score
from sklearn.pipeline import Pipeline

# SageMaker injects these; absent on a laptop, so we fall back to repo paths.
# SM_CHANNEL_TRAIN = where the training data was placed.
TRAIN_DIR = Path(os.environ.get("SM_CHANNEL_TRAIN", "data"))
MODEL_DIR = Path(os.environ.get("SM_MODEL_DIR", "training/model"))


def load_xy(csv_path: Path):
    """Split a labeled CSV into text inputs (X) and labels (y)"""
    df = pd.read_csv(csv_path)
    return df["text"], df["label"]


X_train, y_train = load_xy(TRAIN_DIR / "train.csv")
X_holdout, y_holdout = load_xy(TRAIN_DIR / "holdout.csv")

# One Pipeline is the leakage guard: fit() learns the tfidf vocabulary from
# train only; predict() reuses it on the holdout without ever refitting.
model = Pipeline(
    [
        ("tfidf", TfidfVectorizer(strip_accents="unicode", lowercase=True)),
        ("clf", LogisticRegression(max_iter=1000)),  # default 100 underconverges on tfidf
    ]
)

model.fit(X_train, y_train)

predictions = model.predict(X_holdout)

# pos_label="spam" because our labels are strings, not 0/1. precision and
# recall are defined relative to the positive class, and here "catching spam"
# is that class. Report them separately: for spam, a false positive (real
# message flagged) and a false negative (spam delivered) cost different things.
metrics = {
    "accuracy": accuracy_score(y_holdout, predictions),
    "precision": precision_score(y_holdout, predictions, pos_label="spam"),
    "recall": recall_score(y_holdout, predictions, pos_label="spam"),
    "f1": f1_score(y_holdout, predictions, pos_label="spam"),
}

MODEL_DIR.mkdir(parents=True, exist_ok=True)
joblib.dump(model, MODEL_DIR / "model.joblib")
(MODEL_DIR / "metrics.json").write_text(json.dumps(metrics, indent=2))

print(json.dumps(metrics, indent=2))
