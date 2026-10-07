#!/bin/sh
# compose 스택(memory 공급자)을 저장소에서 빌드해 띄우고 register → update → health를 확인한다.
set -eu
cd "$(dirname "$0")/.."

ENV_FILE=deploy/local.env.example
# shellcheck disable=SC1090
. "./$ENV_FILE"
COMPOSE="docker compose -p sslchk-smoke -f deploy/compose.yaml -f deploy/compose.ports.yaml --env-file $ENV_FILE"
BASE="http://${BIND_ADDR}:${API_PORT}"

trap '$COMPOSE down -v >/dev/null 2>&1' EXIT
$COMPOSE up -d --build

i=0
until [ "$($COMPOSE ps api --format '{{.Health}}')" = "healthy" ]; do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    $COMPOSE logs
    echo "api did not become healthy" >&2
    exit 1
  fi
  sleep 2
done

field() { printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p"; }

reg=$(curl -fsS -X POST "$BASE/register" -H "Authorization: Bearer $ADMIN_KEY" -d '{"domain":"smoke.example"}')
user=$(field "$reg" username)
pass=$(field "$reg" password)
sub=$(field "$reg" subdomain)
txt=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA

resp=$(curl -fsS -X POST "$BASE/update" -H "X-Api-User: $user" -H "X-Api-Key: $pass" \
  -d "{\"subdomain\":\"$sub\",\"txt\":\"$txt\"}")
echo "$resp" | grep -q "\"txt\":\"$txt\"" || { echo "unexpected update response: $resp" >&2; exit 1; }

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/update" -H "X-Api-User: $user" -H "X-Api-Key: wrong" \
  -d "{\"subdomain\":\"$sub\",\"txt\":\"$txt\"}")
[ "$code" = 401 ] || { echo "wrong key returned $code" >&2; exit 1; }

summary=$(curl -fsS "$BASE/api/summary")
echo "$summary" | grep -q '"domains":1' || { echo "unexpected summary: $summary" >&2; exit 1; }
domains=$(curl -fsS "$BASE/api/domains")
echo "$domains" | grep -q '"fqdn":"smoke.example"' || { echo "unexpected domains: $domains" >&2; exit 1; }
echo "$domains" | grep -q '"enabled":true' || { echo "acme not shown as enabled: $domains" >&2; exit 1; }
apicfg=$(curl -fsS "$BASE/api/config")
echo "$apicfg" | grep -q "\"challenge_zone\":\"$CHALLENGE_ZONE\"" || { echo "unexpected config: $apicfg" >&2; exit 1; }

$COMPOSE ps worker --format '{{.State}}' | grep -q running || { $COMPOSE logs worker; echo "worker not running" >&2; exit 1; }
echo "smoke OK"
