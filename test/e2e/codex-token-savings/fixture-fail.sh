#!/bin/sh
# Noisy repository with a large diff and a failing test package.
set -eu
rm -rf /work /tiny
mkdir -p /work/src /work/genok /work/genfail /tiny
cd /work
git init -q
git config user.email e2e@example.com
git config user.name e2e
cat > go.mod <<'MOD'
module example.com/failtests

go 1.25.0
MOD

# 140 passing tests.
{
  echo 'package genok'
  echo
  echo 'import "testing"'
  i=0
  while [ "$i" -lt 140 ]; do
    printf '\nfunc TestOk%03d(t *testing.T) {}\n' "$i"
    i=$((i + 1))
  done
} > genok/ok_test.go

# 140 passing tests plus one failure.
{
  echo 'package genfail'
  echo
  echo 'import "testing"'
  i=0
  while [ "$i" -lt 140 ]; do
    printf '\nfunc TestOk%03d(t *testing.T) {}\n' "$i"
    i=$((i + 1))
  done
  cat <<'TEST'

func TestFailure(t *testing.T) {
	t.Fatalf("expected 42, got 41")
}
TEST
} > genfail/fail_test.go

# Large tracked file that gets a large post-commit change.
i=0
while [ "$i" -lt 300 ]; do
  printf 'baseline line %03d: the quick brown fox jumps over the lazy dog\n' "$i" >> bigdiff.txt
  i=$((i + 1))
done

git add .
git commit -qm baseline

# Rewrite 120 lines to produce a large diff.
i=0
while [ "$i" -lt 120 ]; do
  sed -i "s/^baseline line 0*$i:.*/changed line $i: replaced content for diff noise/" bigdiff.txt
  i=$((i + 1))
done

# Keep the other status noise out of the diff case but still present.
i=0
while [ "$i" -lt 40 ]; do
  printf 'untracked %d\n' "$i" > "untracked_$i.txt"
  i=$((i + 1))
done

# Tiny clean repository.
cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny
