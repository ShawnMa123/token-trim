#!/usr/bin/env bash
# A/B/C token test for failure/diff outputs: baseline vs rtk-go vs rtk.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
TAG=${TAG:-rtk-go-codex-e2e-fail}
RUNS=${RUNS:-6}
RESULTS="${RESULTS_DIR:-$HERE/results-fail}"

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build -f "$HERE/Dockerfile" --build-arg FIXTURE=fixture-fail.sh -t "$TAG" "$ROOT"
fi
rm -rf "$RESULTS"
mkdir -p "$RESULTS"

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
    prompt=$("$HERE/prompt-fail.sh" "$arm")
    out="$RESULTS/${i}-${arm}.jsonl"
    echo ">>> fail-run $i $arm"
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

docker run --rm "$TAG" bench > "$RESULTS/bench.json"
node "$HERE/summarize.js" "$RESULTS" 2
node "$HERE/bench-summarize.js" "$RESULTS/bench.json" > "$RESULTS/bench.md"
cat "$RESULTS/bench.md"
