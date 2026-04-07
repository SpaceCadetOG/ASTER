#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SINCE_DATE="${1:-2026-03-08}"
OUT_FILE="/tmp/live-audit-${SINCE_DATE}-to-now.log"
SUMMARY_FILE="/tmp/live-audit-summary-${SINCE_DATE}.txt"

shopt -s nullglob
selected=()
for f in logs/live-*.log; do
  base="$(basename "$f")"
  day="${base#live-}"
  day="${day%.log}"
  if [[ "$day" > "$SINCE_DATE" || "$day" == "$SINCE_DATE" ]]; then
    selected+=("$f")
  fi
done

if [[ ${#selected[@]} -eq 0 ]]; then
  echo "no matching log files found in ./logs for since=${SINCE_DATE}" >&2
  echo "tip: start runtime with scripts/run_live_safe_logged.sh for conservative live validation" >&2
  exit 1
fi

cat "${selected[@]}" > "$OUT_FILE"

enter_count=$(grep -c "PAPER ENTER" "$OUT_FILE" || true)
exit_count=$(grep -c "PAPER EXIT" "$OUT_FILE" || true)

awk_reason_summary() {
  {
    grep "PAPER EXIT" "$OUT_FILE" | sed -n 's/.*reason=\([^ ]*\).*/\1/p'
    grep "PAPER EXIT" "$OUT_FILE" | sed -n 's/.*Reason:\/<\/strong\> \([^|<]*\).*/\1/p'
  } | sed '/^$/d' | sort | uniq -c | sort -nr
}

awk_symbol_summary() {
  grep "PAPER ENTER\|PAPER EXIT" "$OUT_FILE" \
    | sed -n 's/.*PAPER [A-Z]* | \([A-Z0-9]*\) .*/\1/p' \
    | sed '/^$/d' \
    | sort | uniq -c | sort -nr | head -20
}

{
  echo "since=${SINCE_DATE}"
  echo "source_logs=${#selected[@]}"
  printf '%s\n' "${selected[@]}"
  echo
  echo "paper_enter=${enter_count}"
  echo "paper_exit=${exit_count}"
  echo
  echo "exit_reason_counts:"
  awk_reason_summary
  echo
  echo "top_symbols:"
  awk_symbol_summary
  echo
  echo "hourly_digest_timeline:"
  grep "Hourly Digest" "$OUT_FILE" || true
} > "$SUMMARY_FILE"

cat "$SUMMARY_FILE"
echo
echo "raw_merged_log=${OUT_FILE}"
echo "summary=${SUMMARY_FILE}"
