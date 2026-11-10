#!/usr/bin/env bash
# Run a balanced A/B/C token-savings test: baseline vs rtk-go vs original rtk.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
TAG=${TAG:-rtk-go-codex-e2e}
RUNS=${RUNS:-6}
RESULTS="${RESULTS_DIR:-$HERE/results}"

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build -f "$HERE/Dockerfile" -t "$TAG" "$ROOT"
fi
mkdir -p "$RESULTS"
find "$RESULTS" -maxdepth 1 -type f \( -name "*.jsonl" -o -name "*.err" \) -delete

# Balanced Latin-square-ish order: every arm appears twice in each position
# for a full six-run cycle.
orders=(
  "baseline rtk-go rtk"
  "rtk-go rtk baseline"
  "rtk baseline rtk-go"
  "rtk rtk-go baseline"
  "rtk-go baseline rtk"
  "baseline rtk rtk-go"
)
for i in $(seq 1 "$RUNS"); do
  order=${orders[$(((i - 1) % 6))]}
  for arm in $order; do
    prompt=$("$HERE/prompt.sh" "$arm")
    out="$RESULTS/${i}-${arm}.jsonl"
    echo ">>> run $i $arm"
    for attempt in 1 2; do
      docker run --rm "$TAG" timeout 300 \
        codex exec --json --skip-git-repo-check \
        --dangerously-bypass-approvals-and-sandbox "$prompt" \
        </dev/null >"$out" 2>"$RESULTS/${i}-${arm}.err" || true
      grep -q '"type":"turn.completed"' "$out" && break
      echo "retrying $i $arm (attempt $attempt)" >&2
    done
    grep -q '"type":"turn.completed"' "$out" || { echo "run failed: $out" >&2; tail -5 "$out" >&2; exit 1; }
  done
done

node "$HERE/summarize.js" "$RESULTS"
