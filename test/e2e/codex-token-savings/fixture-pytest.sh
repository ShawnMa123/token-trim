#!/bin/sh
# Pytest project: 120 passing tests, one failing test, skips/warnings.
set -eu
rm -rf /work /tiny
mkdir -p /work/tests /tiny
cd /work
git init -q
git config user.email e2e@example.com
git config user.name e2e
{
  echo 'import pytest'
  i=0
  while [ "$i" -lt 120 ]; do
    printf '\ndef test_pass_%03d():\n    assert True\n' "$i"
    i=$((i + 1))
  done
  cat <<'TEST'

def test_failure():
    expected = 42
    actual = 41
    assert actual == expected, f"expected {expected}, got {actual}"

@pytest.mark.skip(reason="not ready yet")
def test_skipped():
    pass
TEST
} > tests/test_many.py
cat > pytest.ini <<'INI'
[pytest]
addopts = -ra
INI
git add .
git commit -qm baseline

cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny
