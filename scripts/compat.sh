#!/bin/sh
# Start the original acme-dns and ssl-checker (memory provider) and compare their /update responses.
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

# Record the real image digest so it is traceable which acme-dns version a PASS was compared against.
ACMEDNS_REF=$(docker inspect --format '{{.Config.Image}}' sslchk-compat-acmedns-1)
echo "acme-dns image: $ACMEDNS_REF"
docker image inspect --format 'acme-dns digest: {{index .RepoDigests 0}} id: {{.Id}} created: {{.Created}}' "$ACMEDNS_REF"

go test -tags compat -count=1 -v ./test/compat/
