#!/usr/bin/env bash
# Reproduce the complete rtk-go vs original RTK benchmark (sequential).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
RUNS=${RUNS:-6}
PYTEST_RUNS=${PYTEST_RUNS:-4}

cd "$HERE"
RUNS=$RUNS ./run-e2e.sh

docker build --build-arg FIXTURE=fixture-entropy.sh -f Dockerfile -t rtk-go-codex-e2e-entropy "$ROOT"
SKIP_BUILD=1 RUNS=$RUNS TAG=rtk-go-codex-e2e-entropy \
  RESULTS_DIR="$HERE/results-entropy" ./run-e2e.sh

RUNS=$RUNS ./run-fail.sh
RUNS=$PYTEST_RUNS ./run-pytest.sh
RUNS=3 ./run-fix.sh

echo "done; see docs/benchmarks.md and local results*/ summaries"
