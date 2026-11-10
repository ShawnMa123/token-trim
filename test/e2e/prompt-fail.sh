#!/bin/sh
# usage: prompt-fail.sh baseline|rtk-go|rtk
arm=$1
case "$arm" in
  baseline)
    diff_cmd='git diff -- bigdiff.txt'
    test_cmd='go test -count=1 -v ./genfail'
    ;;
  rtk-go)
    diff_cmd='rtk-go exec -- git diff -- bigdiff.txt'
    test_cmd='rtk-go exec -- go test -count=1 -v ./genfail'
    ;;
  rtk)
    diff_cmd='rtk git diff -- bigdiff.txt'
    test_cmd='rtk go test -count=1 -v ./genfail'
    ;;
  *)
    echo "unknown arm: $arm" >&2
    exit 2
    ;;
esac
cat <<PROMPT
You are in /work, a git repository. Run exactly these two shell commands once each, in this order:
1. $diff_cmd
2. $test_cmd

Use only the shell tool for these two commands. Do not read files, do not use any other tool, and do not run any additional shell commands. After both commands have finished, reply with exactly: DONE
PROMPT
