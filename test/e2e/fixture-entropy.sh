#!/bin/sh
# High-entropy noisy repository: random paths and match payloads so that byte
# reduction is less flattered by repetitive-token BPE compression.
set -eu
rm -rf /work /tiny
mkdir -p /work/src/tracked /work/src/search /work/gen /tiny
cd /work
git init -q
git config user.email e2e@example.com
git config user.name e2e
pool=/tmp/rtk-pool
awk 'BEGIN{srand(4242); c="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"; for(i=0;i<3000;i++){s=""; for(j=0;j<72;j++){s=s substr(c,int(rand()*62)+1,1)}; print s}}' > "$pool"
line() { sed -n "${1}p" "$pool"; }

cat > go.mod <<'MOD'
module example.com/entropytests

go 1.25.0
MOD

{
  echo 'package gen'
  echo
  echo 'import "testing"'
  i=0
  while [ "$i" -lt 140 ]; do
    n=$(line $((i + 1)) | cut -c1-10)
    printf '\nfunc TestX%s(t *testing.T) {}\n' "$n"
    i=$((i + 1))
  done
} > gen/gen_test.go

i=0
while [ "$i" -lt 120 ]; do
  n=$(line $((200 + i)) | cut -c1-24)
  printf '%s\n' "$(line $((400 + i)))" > "src/tracked/$n.txt"
  i=$((i + 1))
done

i=0
while [ "$i" -lt 30 ]; do
  mkdir -p "src/search/g$i"
  n=$(line $((700 + i)) | cut -c1-16)
  f="src/search/g$i/$n.txt"
  j=0
  while [ "$j" -lt 20 ]; do
    printf 'TODO_MARKER %s\n' "$(line $((900 + i * 20 + j)))" >> "$f"
    j=$((j + 1))
  done
  i=$((i + 1))
done

git add .
git commit -qm baseline

i=0
while [ "$i" -lt 120 ]; do
  n=$(line $((200 + i)) | cut -c1-24)
  printf '%s\n' "$(line $((1500 + i)))" >> "src/tracked/$n.txt"
  if [ $((i % 3)) -eq 0 ]; then
    git add "src/tracked/$n.txt"
  fi
  i=$((i + 1))
done

i=0
while [ "$i" -lt 80 ]; do
  n=$(line $((1800 + i)) | cut -c1-20)
  printf '%s\n' "$(line $((2100 + i)))" > "untracked_${n}.txt"
  i=$((i + 1))
done

cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny
