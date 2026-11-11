# End-to-end benchmark harness

This directory contains the isolated Docker harness used to compare:

- `baseline`: Codex runs the commands directly;
- `rtk-go`: the same commands via `rtk-go exec --`;
- `rtk`: the original Rust RTK (optional comparison arm).

It runs the real Codex CLI against the local magpie gateway and the local
`my-qoder-local/DeepSeek-Flash` model. No cloud model or provider billing data
is used.

## Fixtures

| fixture | scenario | script |
|---|---|---|
| default | noisy git status, `rg`, verbose passing Go tests | `fixture.sh` / `run-e2e.sh` |
| high entropy | random paths and match bodies | `fixture-entropy.sh` |
| failure + diff | failing Go test package and large diff | `fixture-fail.sh` / `run-fail.sh` |
| pytest | mixed passed/failed/skipped suite | `fixture-pytest.sh` / `run-pytest.sh` |
| bug fix | model must fix a real failing test | `fixture-fix.sh` / `run-fix.sh` |
| long log | repeated log lines + sparse facts + long article | `fixture-longlog.sh` / `run-longlog.sh` |

Every arm uses the same fixture image and a fresh container. Results are written
under `results*/` and are git-ignored.

## Prerequisites

- Docker can reach magpie at `host.docker.internal:3425` with model
  `my-qoder-local/DeepSeek-Flash`.
- The comparison image `rtk-original:0.49.0` is available for the `rtk` arm.
  Build or skip that arm if you only need `baseline` vs `rtk-go`.
- Codex CLI is installed inside the image (`0.163.0`); Go 1.25 is copied from
  the pinned build stage.

The container runs Codex with `--dangerously-bypass-approvals-and-sandbox`
because it is a disposable test box. This does **not** validate host permission
equivalence.

## Run

```sh
cd test/e2e
RUNS=6 ./run-e2e.sh
```

High-entropy:

```sh
docker build --build-arg FIXTURE=fixture-entropy.sh \
  -f test/e2e/Dockerfile -t rtk-go-codex-e2e-entropy .
SKIP_BUILD=1 RUNS=6 TAG=rtk-go-codex-e2e-entropy \
  RESULTS_DIR="$PWD/results-entropy" ./run-e2e.sh
```

Failure/diff, pytest, and bug-fix:

```sh
RUNS=6 ./run-fail.sh
RUNS=4 ./run-pytest.sh
RUNS=3 ./run-fix.sh
RUNS=3 ./run-longlog.sh
```

Everything, including the wrapper microbenchmark:

```sh
./run-all.sh
```

## What is measured

- Codex `turn.completed.usage.input_tokens` / cached / output tokens;
- actual `command_execution.aggregated_output` bytes per command;
- command count and final `DONE`/test status;
- bug-fix verification by running `go test ./...` in a fresh container on the
  same volume.

`input_tokens` includes system prompt, tool framing, and every model turn; it
is not the byte reduction of a single command. Prompt caching is reported
separately.

## Wrapper microbenchmark

```sh
docker run --rm rtk-go-codex-e2e bench > results/bench.json
node bench-summarize.js results/bench.json
```

`bench` runs `git-status`, `rg`, `go-test`, and `tiny-git` under `direct`,
`rtk-go`, and `rtk`: 3 warm-ups + 25 measured runs, reporting p50/p95 wall time,
max RSS (`wait4` rusage), and stdout bytes. The tool is Linux-only.

## Results

The published summary, methodology, and limitations are in
[`../../docs/benchmarks.md`](../../docs/benchmarks.md). Raw JSONL/err/status
files stay local and are not committed.

## Limits

- One model, one Codex version, one prompt shape.
- The `rtk` arm depends on an external comparison image; it is not part of the
  `rtk-go` build or runtime.
- A passing benchmark is not a production guarantee; validate against your own
  repository and model.
