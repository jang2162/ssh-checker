#!/bin/sh
# 원본 acme-dns와 ssl-checker(memory 공급자)를 띄우고 /update 응답을 대조한다.
set -eu
cd "$(dirname "$0")/.."
COMPOSE="docker compose -f test/compat/compose.yaml"

docker build -t ssl-checker:dev .
trap '$COMPOSE down -v >/dev/null 2>&1' EXIT
$COMPOSE up -d

wait_for() {
  i=0
  until curl -s -o /dev/null "$1"; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      $COMPOSE logs
      echo "$1 not reachable" >&2
      exit 1
    fi
    sleep 1
  done
}
wait_for http://127.0.0.1:18080/health
wait_for http://127.0.0.1:18081/health

go test -tags compat -count=1 -v ./test/compat/
