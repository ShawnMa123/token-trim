#!/bin/sh
# usage: prompt-longlog.sh baseline|lossless
arm=$1
case "$arm" in
  baseline) cmd='cat /work/app.log /work/article.txt' ;;
  lossless) cmd='rtk-go exec --profile lossless -- cat /work/app.log /work/article.txt' ;;
  *) echo "unknown arm: $arm" >&2; exit 2 ;;
esac
cat <<PROMPT
You are in /work. Run exactly this shell command once:
$cmd

Use only that command's output. Do not read files, do not run any other
command, and do not use another tool. Answer these questions from the output:
1. How many lines contain the word ERROR?
2. What is the request id on the FATAL line?
3. What is the second stack frame after the FATAL line?
4. What audit window is stated in the article?

Return only one JSON object with keys error_count (number), fatal_request_id,
second_stack_frame, audit_window. Do not add markdown.
PROMPT
