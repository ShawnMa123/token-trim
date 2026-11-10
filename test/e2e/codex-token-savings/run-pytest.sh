#!/usr/bin/env bash
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
TAG=${TAG:-rtk-go-codex-e2e-pytest}
RUNS=${RUNS:-4}
RESULTS="${RESULTS_DIR:-$HERE/results-pytest}"
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build -f "$HERE/Dockerfile" --build-arg FIXTURE=fixture-pytest.sh -t "$TAG" "$ROOT"
fi
rm -rf "$RESULTS"
mkdir -p "$RESULTS"
orders=(
  "baseline rtk-go rtk"
  "rtk-go rtk baseline"
  "rtk baseline rtk-go"
  "rtk rtk-go baseline"
)
for i in $(seq 1 "$RUNS"); do
  order=${orders[$(((i - 1) % 4))]}
  for arm in $order; do
    prompt=$("$HERE/prompt-pytest.sh" "$arm")
    out="$RESULTS/${i}-${arm}.jsonl"
    echo ">>> pytest $i $arm"
    for attempt in 1 2; do
      docker run --rm "$TAG" timeout 300 codex exec --json --skip-git-repo-check \
        --dangerously-bypass-approvals-and-sandbox "$prompt" \
        </dev/null >"$out" 2>"$RESULTS/${i}-${arm}.err" || true
      grep -q '"type":"turn.completed"' "$out" && break
    done
    grep -q '"type":"turn.completed"' "$out" || { echo "run failed: $out" >&2; exit 1; }
  done
done
docker run --rm "$TAG" bench > "$RESULTS/bench.json"
node "$HERE/summarize.js" "$RESULTS" 1 | tee "$RESULTS/summary.txt"
