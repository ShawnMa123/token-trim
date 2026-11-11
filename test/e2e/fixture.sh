#!/bin/sh
# Build a deterministic noisy repository. No host or private data is copied.
set -eu
rm -rf /work
mkdir -p /work/src /work/gen
cd /work
git init -q
git config user.email e2e@example.com
git config user.name e2e

cat > go.mod <<'MOD'
module example.com/noisytests

go 1.25.0
MOD

# 140 fast unit tests with verbose RUN/PASS lines.
{
  echo 'package gen'
  echo
  echo 'import "testing"'
  i=0
  while [ "$i" -lt 140 ]; do
    printf '\nfunc TestCase%03d(t *testing.T) {}\n' "$i"
    i=$((i + 1))
  done
} > gen/gen_test.go

# 120 tracked files, later modified for a noisy git status.
i=0
while [ "$i" -lt 120 ]; do
  mkdir -p "src/tracked$i"
  printf 'package tracked%d\n\nconst Value%d = %d\n' "$i" "$i" "$i" > "src/tracked$i/file_$i.go"
  i=$((i + 1))
done

# Search corpus: 30 files x 20 matches.
i=0
while [ "$i" -lt 30 ]; do
  mkdir -p "src/search$i"
  f="src/search$i/matches_$i.txt"
  j=0
  while [ "$j" -lt 20 ]; do
    printf 'TODO_MARKER group=%d line=%d payload=abcdefghijklmnopqrstuvwxyz0123456789\n' "$i" "$j" >> "$f"
    j=$((j + 1))
  done
  i=$((i + 1))
done

git add .
git commit -qm baseline

# Modify every tracked Go file; stage every third one.
i=0
while [ "$i" -lt 120 ]; do
  printf '\n// modified %d\n' "$i" >> "src/tracked$i/file_$i.go"
  if [ $((i % 3)) -eq 0 ]; then
    git add "src/tracked$i/file_$i.go"
  fi
  i=$((i + 1))
done

# 80 untracked files.
i=0
while [ "$i" -lt 80 ]; do
  printf 'untracked %d\n' "$i" > "untracked_$i.txt"
  i=$((i + 1))
done

# Tiny clean repository for wrapper overhead measurements.
rm -rf /tiny
mkdir -p /tiny
cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny
