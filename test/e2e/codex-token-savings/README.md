# Codex + magpie token-savings E2E

Isolated Docker A/B/C test for `rtk-go` and the original Rust RTK against the
local magpie gateway and the local DeepSeek model. It runs the real Codex CLI
(`0.163.0`), not a mock.

The original RTK binary is built inside Docker from the local `rtk/` source
(version `0.49.0`, commit `8bc263a9`) with Rust 1.91/bookworm, so both wrappers
run on the same Linux image and fixture.

## What is measured

The container image contains a deterministic noisy repository:

- 120 tracked files modified after the baseline commit (40 staged), 80 untracked
  files → noisy `git status`;
- 30 search files × 20 matches → ~36 KiB `rg -n "TODO_MARKER" src`;
- a Go module with 140 trivial tests → verbose `go test -v ./gen` RUN/PASS lines.

Each Codex run receives the same three commands, with one difference:

| arm | commands |
|---|---|
| baseline | `git status`, `rg ...`, `go test -count=1 -v ./gen` |
| rtk-go | same commands prefixed by `rtk-go exec --` |
| rtk | `rtk git status`, `rtk rg ...`, `rtk go test ...` |

Codex is instructed to run exactly those three commands, one at a time, use no
other tool, and reply `DONE`. Runs use a six-cycle balanced order (each arm
appears twice in each position), a fresh container each time, and
`codex exec --json`.

Token usage comes from Codex's own `turn.completed.usage` events
(`input_tokens`, `cached_input_tokens`, `output_tokens`). The test does not
call the magpie admin API and does not use provider billing data.

## Run

Prerequisite: magpie is running on the host and reachable from Docker at
`host.docker.internal:3425` with the model `my-qoder-local/DeepSeek-Flash`.

```sh
cd test/e2e/codex-token-savings
RUNS=6 ./run-e2e.sh
```

High-entropy fixture:

```sh
docker build --build-arg FIXTURE=fixture-entropy.sh -f Dockerfile -t rtk-go-codex-e2e-entropy ../..
SKIP_BUILD=1 RUNS=6 TAG=rtk-go-codex-e2e-entropy RESULTS_DIR="$PWD/results-entropy" ./run-e2e.sh
```

Failure/diff fixture:

```sh
RUNS=6 ./run-fail.sh
```

Actual results are in [`RESULTS.md`](RESULTS.md).

`run-e2e.sh` builds `go/` with Go 1.25 inside the image, installs Codex CLI,
builds the original RTK from `rtk/`, and runs the A/B/C pairs. Results are
written to `results/` (git-ignored).

## Wrapper microbenchmark

```sh
docker run --rm rtk-go-codex-e2e bench > results/bench.json
node bench-summarize.js results/bench.json
```

`bench` runs four cases (`git-status`, `rg`, `go-test`, `tiny-git`) under
`direct`, `rtk-go`, and `rtk`: 3 warm-ups + 25 measured runs each. It reports
p50/p95 wall time, max RSS from `wait4` rusage, and stdout bytes.

## Limits

- One model (`my-qoder-local/DeepSeek-Flash`), one Codex version, one prompt.
- The container is intentionally run with `--dangerously-bypass-approvals-and-sandbox`
  because it is an isolated test box; this does not validate host permission
  equivalence.
- Provider prompt caching can affect both arms; cached input tokens are
  reported separately.
- `input_tokens` includes system/tool framing and every model turn, so the
  reduction is not the same as the byte reduction of a single command output.
