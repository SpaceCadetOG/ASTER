# ML Runtime

ASTER's runtime scorer is Go-native.

That means:

- `cmd/live` loads the model JSON directly in Go
- candidate feature extraction happens in Go
- scoring, ranking, filtering, and shadow logging happen in Go
- Python is only optional for offline training/export

## Runtime env

```bash
LIVE_ML_ENABLE=true
LIVE_ML_MODE=shadow
LIVE_ML_MODEL_PATH=models/ml/take_trade_v1.example.json
LIVE_ML_MIN_TAKE_PROB=0.55
LIVE_ML_MIN_EXPECTED_R=0.20
LIVE_ML_SHADOW_LOG=true
```

Modes:

- `shadow`: log ML scores only
- `rank`: adjust candidate `FinalRank`
- `filter`: reject weak candidates after rules build the setup
- `manage`: suggest management profile only

Recommended first mode:

- `shadow`

## Native model validation

Validate a model file entirely in Go:

```bash
go run ./cmd/mlvalidate -model models/ml/take_trade_v1.example.json
```

Score a manual payload in Go:

```bash
go run ./cmd/mlvalidate \
  -model models/ml/take_trade_v1.example.json \
  -features '{"combined_score":0.82,"volume_ratio":1.6,"ofi_z":1.1,"book_imbalance":0.35}' \
  -categories '{"side":"BUY","strategy_id":"vp_trend","setup_family":"cont","session_label":"new_york","entry_timing":"fresh"}'
```

## Runtime insertion point

ASTER flow:

1. scanner builds rule candidates
2. strategy enrichment computes ASTER features
3. Go ML scorer scores candidate
4. hard gate / throttle / risk still decide safety
5. paper/live execution continues unchanged

ML does not bypass risk.

## Native training

Train a first-pass logistic model in Go:

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/mltrain-linear \
  -input data/ml/trade_features.csv \
  -target win \
  -out models/ml/take_trade_v1.local.json \
  -model-id take_trade_v1_local \
  -epochs 500 \
  -lr 0.05 \
  -l2 0.001
```

## Model schema

The runtime currently expects a linear JSON model with:

- `weights`: numeric feature weights
- `category_biases`: categorical offsets
- `outputs`: scaling and profile thresholds

See:

- [models/ml/take_trade_v1.example.json](/Users/victorogbebor/2026/go-machine/models/ml/take_trade_v1.example.json:1)
