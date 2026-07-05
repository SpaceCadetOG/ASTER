#!/usr/bin/env python3
import argparse
import json
import math

import pandas as pd


def sigmoid(x: float) -> float:
    if x > 30:
        return 1.0
    if x < -30:
        return 0.0
    return 1.0 / (1.0 + math.exp(-x))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--model", required=True)
    args = parser.parse_args()

    df = pd.read_csv(args.input)
    model = json.loads(open(args.model, "r", encoding="utf-8").read())
    weights = model.get("weights", {})
    cat_biases = model.get("category_biases", {})
    bias = float(model.get("bias", 0.0))

    preds = []
    for _, row in df.iterrows():
        score = bias
        for name, weight in weights.items():
            score += float(row.get(name, 0.0)) * float(weight)
        for col, mapping in cat_biases.items():
            score += float(mapping.get(str(row.get(col, "")), 0.0))
        preds.append(sigmoid(score))

    df["pred_take_prob"] = preds
    print(df[["trade_id", "symbol", "win", "pred_take_prob"]].head(20).to_string(index=False))
    print(f"rows={len(df)} avg_pred={df['pred_take_prob'].mean():.4f}")


if __name__ == "__main__":
    main()
