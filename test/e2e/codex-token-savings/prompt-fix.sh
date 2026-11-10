#!/bin/sh
arm=$1
case "$arm" in
  baseline) hint='Run `go test ./...` normally.' ;;
  rtk-go)   hint='Always run tests through `rtk-go exec -- go test ./...`.' ;;
  rtk)      hint='Always run tests through `rtk go test ./...`.' ;;
  *) echo "unknown arm: $arm" >&2; exit 2 ;;
esac
cat <<PROMPT
You are in /work, a Go module. $hint
Run the tests to see the failure, fix the bug in calc.go (do not change tests), and rerun until all tests pass. Reply exactly DONE when they pass.
PROMPT
