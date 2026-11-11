#!/usr/bin/env bash
# Compare raw long log/article output with the lossless repeat filter.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
TAG=${TAG:-rtk-go-codex-e2e-longlog}
RUNS=${RUNS:-3}
RESULTS="${RESULTS_DIR:-$HERE/results-longlog}"
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build --build-arg FIXTURE=fixture-longlog.sh -f "$HERE/Dockerfile" -t "$TAG" "$ROOT"
fi
rm -rf "$RESULTS"
mkdir -p "$RESULTS"
for i in $(seq 1 "$RUNS"); do
  for arm in baseline lossless; do
    prompt=$("$HERE/prompt-longlog.sh" "$arm")
    out="$RESULTS/${i}-${arm}.jsonl"
    echo ">>> longlog $i $arm"
    docker run --rm "$TAG" timeout 300 codex exec --json --skip-git-repo-check \
      --dangerously-bypass-approvals-and-sandbox "$prompt" \
      </dev/null >"$out" 2>"$RESULTS/${i}-${arm}.err" || true
    grep -q '"type":"turn.completed"' "$out" || { echo "run failed: $out" >&2; exit 1; }
  done
done
node "$HERE/summarize-longlog.js" "$RESULTS"
