#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import pandas as pd
from sklearn.compose import ColumnTransformer
from sklearn.impute import SimpleImputer
from sklearn.linear_model import LogisticRegression
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import OneHotEncoder, StandardScaler


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    df = pd.read_csv(args.input)
    target = (df["win"].astype(float) > 0).astype(int)

    cat_cols = ["side", "strategy_id", "setup_family", "session_label", "entry_timing"]
    num_cols = [
        "day_utc_24h_pct",
        "utc_4h_pct",
        "utc_1h_pct",
        "volume_ratio",
        "distance_to_vwap_pct",
        "atr_pct",
        "extension_atr",
        "spread_bps",
        "book_imbalance",
        "ofi_z",
        "combined_score",
        "trade_quality",
        "candidate_age_seconds",
    ]

    pre = ColumnTransformer(
        transformers=[
            (
                "num",
                Pipeline(
                    steps=[
                        ("impute", SimpleImputer(strategy="constant", fill_value=0.0)),
                        ("scale", StandardScaler()),
                    ]
                ),
                num_cols,
            ),
            (
                "cat",
                Pipeline(
                    steps=[
                        ("impute", SimpleImputer(strategy="constant", fill_value="unknown")),
                        ("onehot", OneHotEncoder(handle_unknown="ignore")),
                    ]
                ),
                cat_cols,
            ),
        ]
    )

    pipe = Pipeline(
        steps=[
            ("pre", pre),
            ("model", LogisticRegression(max_iter=2000)),
        ]
    )
    pipe.fit(df[cat_cols + num_cols], target)

    model = pipe.named_steps["model"]
    preprocessor = pipe.named_steps["pre"]
    feature_names = preprocessor.get_feature_names_out()
    weights = {}
    category_biases = {}
    for name, coef in zip(feature_names, model.coef_[0]):
        if name.startswith("num__"):
            weights[name.replace("num__", "", 1)] = float(coef)
            continue
        if name.startswith("cat__"):
            raw = name.replace("cat__", "", 1)
            column = next((col for col in cat_cols if raw.startswith(col + "_")), None)
            if column is None:
                continue
            category = raw[len(column) + 1 :]
            category_biases.setdefault(column, {})[category] = float(coef)

    export = {
        "model_type": "linear",
        "model_version": "take_trade_v1",
        "bias": float(model.intercept_[0]),
        "weights": weights,
        "category_biases": category_biases,
        "outputs": {
            "probability_scale": 1.0,
            "expected_r_scale": 3.0,
            "expected_max_r_scale": 2.0,
            "reclaim_scale": 1.0,
            "reentry_scale": 1.0,
            "stop_profiles": [
                {"min_score": 0.75, "label": "wide"},
                {"min_score": 0.35, "label": "normal"},
                {"min_score": -999, "label": "tight"},
            ],
            "exit_profiles": [
                {"min_score": 1.50, "label": "runner"},
                {"min_score": 0.75, "label": "balanced"},
                {"min_score": -999, "label": "fast"},
            ],
        },
    }

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(json.dumps(export, indent=2))
    print(f"wrote {out_path}")


if __name__ == "__main__":
    main()
