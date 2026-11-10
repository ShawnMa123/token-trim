#!/bin/sh
# usage: prompt.sh baseline|rtk-go|rtk
arm=$1
case "$arm" in
  baseline)
    git_cmd='git status'
    rg_cmd='rg -n "TODO_MARKER" src'
    go_cmd='go test -count=1 -v ./gen'
    ;;
  rtk-go)
    git_cmd='rtk-go exec -- git status'
    rg_cmd='rtk-go exec -- rg -n "TODO_MARKER" src'
    go_cmd='rtk-go exec -- go test -count=1 -v ./gen'
    ;;
  rtk)
    git_cmd='rtk git status'
    rg_cmd='rtk rg -n "TODO_MARKER" src'
    go_cmd='rtk go test -count=1 -v ./gen'
    ;;
  *)
    echo "unknown arm: $arm" >&2
    exit 2
    ;;
esac
cat <<PROMPT
You are in /work, a git repository. Run exactly these three shell commands once each, in this order:
1. $git_cmd
2. $rg_cmd
3. $go_cmd

Use only the shell tool for these three commands. Do not read files, do not use any other tool, and do not run any additional shell commands. After all three commands have finished, reply with exactly: DONE
PROMPT
