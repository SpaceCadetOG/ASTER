#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_TZ="${ASTER_LOG_TZ:-America/Chicago}"
today_in_tz() {
  TZ="$LOG_TZ" date +%F
}

SINCE_DATE="${1:-$(today_in_tz)}"
UNTIL_DATE="${2:-$(today_in_tz)}"
OUT_DIR="out/agent_handoff"
LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$OUT_DIR"

OUT_FILE="${OUT_DIR}/agent-handoff-${SINCE_DATE}-to-${UNTIL_DATE}.md"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

LIVE_MERGED="${TMP_DIR}/live-merged.log"
LONG_MERGED="${TMP_DIR}/long-merged.log"
SHORT_MERGED="${TMP_DIR}/short-merged.log"
STATS_OUT="${TMP_DIR}/stats.txt"

merge_logs() {
  local pattern="$1"
  local out="$2"
  : > "$out"
  shopt -s nullglob
  local selected=()
  for f in "${LOG_DIR}"/${pattern}; do
    local base day
    base="$(basename "$f")"
    day="$(printf '%s' "$base" | sed -E 's/^[^-]+-([0-9]{4}-[0-9]{2}-[0-9]{2}).*/\1/')"
    if [[ "$day" > "$UNTIL_DATE" || "$day" < "$SINCE_DATE" ]]; then
      continue
    fi
    selected+=("$f")
  done
  if [[ ${#selected[@]} -gt 0 ]]; then
    cat "${selected[@]}" > "$out"
  fi
}

merge_logs "live-*.log" "$LIVE_MERGED"
merge_logs "long-*.log" "$LONG_MERGED"
merge_logs "short-*.log" "$SHORT_MERGED"

EVENTS_FILE="${LOG_DIR}/events.jsonl"

if [[ -f "$EVENTS_FILE" ]]; then
  if ! /bin/zsh -lc "GOCACHE=$(pwd)/.gocache go run ./cmd/stats -log ${EVENTS_FILE} -from ${SINCE_DATE} -to ${UNTIL_DATE}" > "$STATS_OUT" 2>/dev/null; then
    : > "$STATS_OUT"
  fi
else
  : > "$STATS_OUT"
fi

paper_recent_csv() {
  if [[ ! -f out/paper_trades.csv ]]; then
    return 0
  fi
  awk -F, -v since="$SINCE_DATE" -v until="$UNTIL_DATE" '
    NR==1 { next }
    {
      d=substr($1,1,10)
      if (d >= since && d <= until) print
    }
  ' out/paper_trades.csv
}

live_recent_csv() {
  if [[ ! -f out/live_trades.csv ]]; then
    return 0
  fi
  awk -F, -v since="$SINCE_DATE" -v until="$UNTIL_DATE" '
    NR==1 { next }
    {
      d=substr($1,1,10)
      if (d >= since && d <= until) print
    }
  ' out/live_trades.csv
}

paper_exit_reason_counts() {
  if [[ ! -f out/paper_trades.csv ]]; then
    return 0
  fi
  paper_recent_csv | awk -F, '{print $11}' | sed '/^$/d' | sort | uniq -c | sort -nr
}

paper_top_symbols() {
  if [[ ! -f out/paper_trades.csv ]]; then
    return 0
  fi
  paper_recent_csv | awk -F, '{count[$2]++; pnl[$2]+=$14} END {for (s in count) printf "%7d %12.2f %s\n", count[s], pnl[s], s}' | sort -k1,1nr -k2,2nr | head -20
}

latest_hourly_digests() {
  if [[ ! -s "$LIVE_MERGED" ]]; then
    return 0
  fi
  grep "Hourly Digest" "$LIVE_MERGED" | tail -40 || true
}

latest_rejects() {
  if [[ ! -s "$LIVE_MERGED" ]]; then
    return 0
  fi
  grep -E "skip \\(|Reject|reason=" "$LIVE_MERGED" | tail -80 || true
}

recent_entries_exits() {
  if [[ ! -s "$LIVE_MERGED" ]]; then
    return 0
  fi
  grep -E "PAPER ENTER|PAPER EXIT|ORDER ERROR|ORDER PLACED|top candidate|signal: none" "$LIVE_MERGED" | tail -120 || true
}

scanner_tail() {
  local file="$1"
  if [[ ! -s "$file" ]]; then
    return 0
  fi
  tail -120 "$file"
}

{
  echo "# ASTER Daily Agent Handoff"
  echo
  echo "- Generated: $(date -u +%FT%TZ)"
  echo "- Window: ${SINCE_DATE} to ${UNTIL_DATE}"
  echo "- Repo: ${ROOT_DIR}"
  echo
  echo "## File Presence"
  echo
  for f in \
    "${EVENTS_FILE}" \
    "out/paper_trades.csv" \
    "out/live_trades.csv" \
    "out/paper_equity.csv" \
    "$LIVE_MERGED" \
    "$LONG_MERGED" \
    "$SHORT_MERGED"; do
    if [[ -f "$f" ]]; then
      echo "- ${f}: present"
    else
      echo "- ${f}: missing"
    fi
  done
  echo
  echo "## Event Stats"
  echo
  if [[ -s "$STATS_OUT" ]]; then
    echo '```text'
    cat "$STATS_OUT"
    echo '```'
  else
    echo "_No event stats available._"
  fi
  echo
  echo "## Paper Exit Reasons"
  echo
  if [[ -f out/paper_trades.csv ]]; then
    echo '```text'
    paper_exit_reason_counts || true
    echo '```'
  else
    echo "_No paper trades file._"
  fi
  echo
  echo "## Top Paper Symbols"
  echo
  if [[ -f out/paper_trades.csv ]]; then
    echo '```text'
    echo " trades      net_pnl symbol"
    paper_top_symbols || true
    echo '```'
  else
    echo "_No paper trades file._"
  fi
  echo
  echo "## Recent Live-Lite Timeline"
  echo
  echo '```text'
  recent_entries_exits || true
  echo '```'
  echo
  echo "## Recent Hourly Digests"
  echo
  echo '```text'
  latest_hourly_digests || true
  echo '```'
  echo
  echo "## Recent Rejects / Skip Reasons"
  echo
  echo '```text'
  latest_rejects || true
  echo '```'
  echo
  echo "## Long Scanner Tail"
  echo
  echo '```text'
  scanner_tail "$LONG_MERGED"
  echo '```'
  echo
  echo "## Short Scanner Tail"
  echo
  echo '```text'
  scanner_tail "$SHORT_MERGED"
  echo '```'
  echo
  echo "## Recent Paper Trades"
  echo
  echo '```csv'
  if [[ -f out/paper_trades.csv ]]; then
    sed -n '1p' out/paper_trades.csv
    paper_recent_csv | tail -80
  fi
  echo '```'
  echo
  echo "## Recent Live Trades"
  echo
  echo '```csv'
  if [[ -f out/live_trades.csv ]]; then
    sed -n '1p' out/live_trades.csv
    live_recent_csv | tail -80
  fi
  echo '```'
} > "$OUT_FILE"

echo "$OUT_FILE"
