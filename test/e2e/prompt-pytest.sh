#!/bin/sh
arm=$1
case "$arm" in
  baseline) cmd='pytest tests' ;;
  rtk-go)   cmd='rtk-go exec -- pytest tests' ;;
  rtk)      cmd='rtk pytest tests' ;;
  *) echo "unknown arm: $arm" >&2; exit 2 ;;
esac
cat <<PROMPT
You are in /work. Run exactly this one shell command once: $cmd
Use only the shell tool, do not read files or run anything else. Then reply exactly DONE
PROMPT
