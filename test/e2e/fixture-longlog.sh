#!/bin/sh
# Long-log / long-text fixture for the lossless profile. The log deliberately
# contains large runs of identical lines plus sparse unique facts; the article
# has no repeated line, so a lossless filter must pass it through.
set -eu
rm -rf /work /tiny
mkdir -p /work /tiny
cd /work

{
heartbeat='2026-11-11T00:00:00Z INFO worker heartbeat queue=default state=idle attempts=0 latency=12ms'
i=0
while [ "$i" -lt 1000 ]; do
  echo "$heartbeat"
  i=$((i + 1))
done

echo '2026-11-11T00:01:00Z INFO request id=REQ-0001 path=/api/health status=200'
echo '2026-11-11T00:01:01Z WARN request id=REQ-0002 path=/api/old status=404'
echo '2026-11-11T00:01:02Z ERROR request id=REQ-0003 path=/api/orders status=500 code=E42'
echo '2026-11-11T00:01:03Z INFO request id=REQ-0004 path=/api/health status=200'
echo '2026-11-11T00:01:04Z ERROR request id=REQ-0005 path=/api/checkout status=500 code=E43'
echo '2026-11-11T00:01:05Z INFO request id=REQ-0006 path=/api/health status=200'

i=0
while [ "$i" -lt 1000 ]; do
  echo "$heartbeat"
  i=$((i + 1))
done

echo '2026-11-11T00:02:00Z FATAL request id=REQ-7391 path=/api/payments code=E42'
echo 'goroutine 1 [running]:'
echo 'worker.process(/srv/app/worker.go:214)'
echo 'worker.(*Server).Serve(/srv/app/server.go:88)'
echo 'main.main(/srv/app/main.go:31)'

i=0
while [ "$i" -lt 1000 ]; do
  echo "$heartbeat"
  i=$((i + 1))
done

echo '2026-11-11T00:03:00Z ERROR request id=REQ-9001 path=/api/search status=500 code=E44'

} > /work/app.log

{
  echo '# Quarterly operations note'
  echo
  i=0
  while [ "$i" -lt 120 ]; do
    echo "Paragraph $i describes routine queue maintenance and deployment checks without any special marker."
    i=$((i + 1))
  done
  echo 'The audit window closes at 03:17 UTC, and the retention owner is the platform team.'
  i=0
  while [ "$i" -lt 120 ]; do
    echo "Appendix $i repeats ordinary archival guidance and contains no critical numeric fact."
    i=$((i + 1))
  done
} > article.txt

cd /tiny
git init -q
git config user.email e2e@example.com
git config user.name e2e
echo tiny > file.txt
git add file.txt
git commit -qm tiny

cat > /work/TASK_FACTS.txt <<'EOF'
error_count=3
fatal_request_id=REQ-7391
second_stack_frame=worker.(*Server).Serve(/srv/app/server.go:88)
audit_window=03:17 UTC
EOF
