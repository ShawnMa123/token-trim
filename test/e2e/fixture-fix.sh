#!/bin/sh
# Small bug-fix task with noisy passing tests and one real failure.
set -eu
rm -rf /work /tiny
mkdir -p /work /tiny
cd /work
git init -q
git config user.email e2e@example.com
git config user.name e2e
cat > go.mod <<'MOD'
module example.com/fixtask

go 1.25.0
MOD
cat > calc.go <<'GO'
package fixtask

func Add(a, b int) int {
	return a - b
}
GO
{
  echo 'package fixtask'
  echo
  echo 'import "testing"'
  i=0
  while [ "$i" -lt 120 ]; do
    printf '\nfunc TestNoise%03d(t *testing.T) {}\n' "$i"
    i=$((i + 1))
  done
  cat <<'TEST'

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2,3) = %d, want 5", got)
	}
}
TEST
} > calc_test.go
git add .
git commit -qm baseline

cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny
