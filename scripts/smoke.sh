#!/usr/bin/env bash
set -euo pipefail

# --- Config (override via env) ---
HOST_LONG="${HOST_LONG:-http://127.0.0.1:8080}"
HOST_SHORT="${HOST_SHORT:-http://127.0.0.1:8081}"
SYM_LONG="${SYM_LONG:-BTCUSDT}"
SYM_SHORT="${SYM_SHORT:-ETHUSDT}"
TF="${TF:-15m}"
N="${N:-200}"
LEFT="${LEFT:-3}"
RIGHT="${RIGHT:-3}"
WIN="${WIN:-20}"
ZMIN="${ZMIN:-2.0}"
VMIN="${VMIN:-5000000}"
LEVELS="${LEVELS:-50}"

# --- Pretty printing ---
GREEN() { printf "\033[32m%s\033[0m\n" "$*"; }
RED()   { printf "\033[31m%s\033[0m\n" "$*"; }
YELLOW(){ printf "\033[33m%s\033[0m\n" "$*"; }
BOLD()  { printf "\033[1m%s\033[0m\n" "$*"; }

check_json_key() {
  local json="$1" key="$2"
  jq -e ".$key" >/dev/null 2>&1 <<<"$json"
}

curl_json() {
  curl -fsS --max-time 10 "$1"
}

fail=0

BOLD "=== Smoke: health endpoints ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  if out=$(curl -fsS --max-time 5 "$base/health" 2>/dev/null); then
    GREEN "OK  $base/health $out"
  else
    RED "ERR $base/health"
    fail=$((fail+1))
  fi
done

BOLD "=== Smoke: candles ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  sym=$SYM_LONG
  [[ "$base" == "$HOST_SHORT" ]] && sym=$SYM_SHORT
  url="$base/api/candles?symbol=$sym&tf=$TF&n=$N"
  if out=$(curl_json "$url"); then
    if check_json_key "$out" data[0].t; then
      GREEN "OK  $url  (candles: $(jq '.data|length' <<<"$out"))"
    else
      YELLOW "WARN $url returned JSON but missing candles shape"
      fail=$((fail+1))
    fi
  else
    RED "ERR $url"
    fail=$((fail+1))
  fi
done

BOLD "=== Smoke: pivots / structure ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  sym=$SYM_LONG
  [[ "$base" == "$HOST_SHORT" ]] && sym=$SYM_SHORT

  piv="$base/api/pivots?symbol=$sym&tf=$TF&n=$N&left=$LEFT&right=$RIGHT"
  if out=$(curl_json "$piv"); then
    GREEN "OK  pivots (count: $(jq '.data|length' <<<"$out"))"
  else
    RED "ERR $piv"; fail=$((fail+1))
  fi

  str="$base/api/structure?symbol=$sym&tf=$TF&n=$N&left=$LEFT&right=$RIGHT"
  if out=$(curl_json "$str"); then
    GREEN "OK  structure (legs: $(jq '.swings|length' <<<"$out" 2>/dev/null || echo '?'))"
  else
    RED "ERR $str"; fail=$((fail+1))
  fi
done

BOLD "=== Smoke: patterns ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  sym=$SYM_LONG
  [[ "$base" == "$HOST_SHORT" ]] && sym=$SYM_SHORT
  pat="$base/api/patterns?symbol=$sym&tf=$TF&n=$N"
  if out=$(curl_json "$pat"); then
    GREEN "OK  patterns (tags: $(jq '.tags|length' <<<"$out" 2>/dev/null || echo '?'))"
  else
    RED "ERR $pat"; fail=$((fail+1))
  fi
done

BOLD "=== Smoke: volstats (VWAP & spikes) ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  sym=$SYM_LONG
  [[ "$base" == "$HOST_SHORT" ]] && sym=$SYM_SHORT
  vol="$base/api/volstats?symbol=$sym&tf=$TF&n=$N&win=$WIN&zmin=$ZMIN&vmin=$VMIN"
  if out=$(curl_json "$vol"); then
    GREEN "OK  volstats (spikes: $(jq '.spikes|length' <<<"$out"))"
  else
    RED "ERR $vol"; fail=$((fail+1))
  fi
done

BOLD "=== Smoke: confluence (fusion of TR/EF/OB) ==="
for base in "$HOST_LONG" "$HOST_SHORT"; do
  sym=$SYM_LONG
  side="long"
  [[ "$base" == "$HOST_SHORT" ]] && { sym=$SYM_SHORT; side="short"; }
  conf="$base/api/confluence?symbol=$sym&tf=$TF&n=$N&win=$WIN&zmin=$ZMIN&vmin=$VMIN&levels=$LEVELS&side=$side"
  if out=$(curl_json "$conf"); then
    score=$(jq -r '.score // .confluenceScore // empty' <<<"$out")
    label=$(jq -r '.label // .grade // empty' <<<"$out")
    GREEN "OK  confluence side=$side score=${score:-?} label=${label:-?}"
  else
    RED "ERR $conf"; fail=$((fail+1))
  fi
done

echo
if (( fail == 0 )); then
  GREEN "✓ All smoke checks passed."
  exit 0
else
  RED "✗ Smoke checks failed ($fail). See errors above."
  exit 1
fi