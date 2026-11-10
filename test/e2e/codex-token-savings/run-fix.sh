#!/usr/bin/env bash
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
TAG=${TAG:-rtk-go-codex-e2e-fix}
RUNS=${RUNS:-3}
RESULTS="${RESULTS_DIR:-$HERE/results-fix}"
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build -f "$HERE/Dockerfile" --build-arg FIXTURE=fixture-fix.sh -t "$TAG" "$ROOT"
fi
rm -rf "$RESULTS"
mkdir -p "$RESULTS"
orders=("baseline rtk-go rtk" "rtk-go rtk baseline" "rtk baseline rtk-go" "rtk rtk-go baseline" "rtk-go baseline rtk" "baseline rtk rtk-go")
for i in $(seq 1 "$RUNS"); do
  order=${orders[$(((i - 1) % 6))]}
  for arm in $order; do
    vol="fix-$i-$arm-$$"
    docker volume create "$vol" >/dev/null
    prompt=$("$HERE/prompt-fix.sh" "$arm")
    out="$RESULTS/${i}-${arm}.jsonl"
    echo ">>> fix $i $arm"
    docker run --rm -v "$vol:/work" "$TAG" timeout 600 codex exec --json --skip-git-repo-check \
      --dangerously-bypass-approvals-and-sandbox "$prompt" \
      </dev/null >"$out" 2>"$RESULTS/${i}-${arm}.err" || true
    if docker run --rm -v "$vol:/work" "$TAG" go test ./... >"$RESULTS/${i}-${arm}.verify" 2>&1; then
      echo pass > "$RESULTS/${i}-${arm}.status"
    else
      echo fail > "$RESULTS/${i}-${arm}.status"
    fi
    docker volume rm "$vol" >/dev/null 2>&1 || true
  done
done
node "$HERE/summarize.js" "$RESULTS" 5 || true
for f in "$RESULTS"/*.status; do echo "$(basename "$f"): $(cat "$f")"; done
