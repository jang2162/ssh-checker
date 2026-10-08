#!/bin/sh
# compose 스택(memory 공급자)을 저장소에서 빌드해 띄우고 register → update → 관리 API → 화면·llms.txt를 확인한다.
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

# 인증서를 받는 서버용 경로는 /acme/ 아래에서도 같은 동작이다. verify는 TXT를 건드리지 않는다.
resp=$(curl -fsS -X POST "$BASE/acme/update" -H "X-Api-User: $user" -H "X-Api-Key: $pass" \
  -d "{\"subdomain\":\"$sub\",\"txt\":\"$txt\"}")
echo "$resp" | grep -q "\"txt\":\"$txt\"" || { echo "unexpected /acme/update response: $resp" >&2; exit 1; }
resp=$(curl -fsS -X POST "$BASE/acme/verify" -H "X-Api-User: $user" -H "X-Api-Key: $pass")
echo "$resp" | grep -q '"ok":true' || { echo "unexpected /acme/verify response: $resp" >&2; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/acme/verify" -H "X-Api-User: $user" -H "X-Api-Key: wrong")
[ "$code" = 401 ] || { echo "verify with wrong key returned $code" >&2; exit 1; }
# 로그인 없이 내려받는 설치·세팅 스크립트: 셸 문법이 맞고, 비밀값이 없다.
for f in install.sh setup.sh; do
  curl -fsS "$BASE/acme/$f" -o "/tmp/smoke-$f"
  bash -n "/tmp/smoke-$f" || { echo "/acme/$f is not valid shell" >&2; exit 1; }
  ! grep -q "$pass" "/tmp/smoke-$f" || { echo "/acme/$f contains the password" >&2; exit 1; }
done
grep -q "verify_server" /tmp/smoke-setup.sh || { echo "/acme/setup.sh missing verify step" >&2; exit 1; }
rm -f /tmp/smoke-install.sh /tmp/smoke-setup.sh

summary=$(curl -fsS "$BASE/api/summary")
echo "$summary" | grep -q '"domains":1' || { echo "unexpected summary: $summary" >&2; exit 1; }
domains=$(curl -fsS "$BASE/api/domains")
echo "$domains" | grep -q '"fqdn":"smoke.example"' || { echo "unexpected domains: $domains" >&2; exit 1; }
echo "$domains" | grep -q '"enabled":true' || { echo "acme not shown as enabled: $domains" >&2; exit 1; }
apicfg=$(curl -fsS "$BASE/api/config")
echo "$apicfg" | grep -q "\"challenge_zone\":\"$CHALLENGE_ZONE\"" || { echo "unexpected config: $apicfg" >&2; exit 1; }

auth="Authorization: Bearer $ADMIN_KEY"
json="Content-Type: application/json"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/domains" -H "$json" -d '{"fqdn":"smoke2.example"}')
[ "$code" = 401 ] || { echo "unauthenticated write returned $code" >&2; exit 1; }

dom=$(curl -fsS -X POST "$BASE/api/domains" -H "$auth" -H "$json" -d '{"fqdn":"smoke2.example"}')
did=$(printf '%s' "$dom" | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
[ -n "$did" ] || { echo "unexpected domain response: $dom" >&2; exit 1; }

creds=$(curl -fsS -X POST "$BASE/api/domains/$did/acme" -H "$auth" -H "$json" -d '{}')
user2=$(field "$creds" username)
pass2=$(field "$creds" password)
sub2=$(field "$creds" subdomain)
resp=$(curl -fsS -X POST "$BASE/update" -H "X-Api-User: $user2" -H "X-Api-Key: $pass2" \
  -d "{\"subdomain\":\"$sub2\",\"txt\":\"$txt\"}")
echo "$resp" | grep -q "\"txt\":\"$txt\"" || { echo "update with API-issued creds failed: $resp" >&2; exit 1; }

rot=$(curl -fsS -X POST "$BASE/api/domains/$did/acme/rotate" -H "$auth" -H "$json")
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/update" -H "X-Api-User: $user2" -H "X-Api-Key: $pass2" \
  -d "{\"subdomain\":\"$sub2\",\"txt\":\"$txt\"}")
[ "$code" = 401 ] || { echo "old key after rotate returned $code" >&2; exit 1; }
[ "$(field "$rot" subdomain)" = "$sub2" ] || { echo "rotate changed subdomain: $rot" >&2; exit 1; }

jar=$(mktemp)
curl -fsS -c "$jar" -X POST "$BASE/api/session" -H "$json" -d "{\"key\":\"$ADMIN_KEY\"}" >/dev/null
curl -fsS -b "$jar" "$BASE/api/session" | grep -q '"admin":true' || { echo "session cookie not accepted" >&2; exit 1; }
rm -f "$jar"

# 화면과 llms.txt. 화면 파일은 이미지를 빌드할 때 내장된다.
ui=$(curl -fsS -o /dev/null -w '%{http_code} %{content_type}' "$BASE/")
case "$ui" in "200 text/html"*) ;; *) echo "dashboard not served: $ui" >&2; exit 1 ;; esac
spa=$(curl -fsS -o /dev/null -w '%{http_code}' "$BASE/domains/1")
[ "$spa" = 200 ] || { echo "SPA fallback returned $spa" >&2; exit 1; }
js=$(curl -fsS "$BASE/" | sed -n 's/.*src="\(\/assets\/[^"]*\.js\)".*/\1/p')
[ -n "$js" ] || { echo "no script in index.html" >&2; exit 1; }
curl -fsS -D - -o /dev/null "$BASE$js" | grep -qi 'cache-control: public, max-age=31536000, immutable' || { echo "asset cache header missing" >&2; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/assets/missing.js")
[ "$code" = 404 ] || { echo "missing asset returned $code" >&2; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/update")
[ "$code" = 405 ] || { echo "GET /update returned $code (화면이 API 경로를 가로챘다)" >&2; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/acme/update")
[ "$code" = 405 ] || { echo "GET /acme/update returned $code" >&2; exit 1; }
llms=$(curl -fsS "$BASE/llms.txt")
echo "$llms" | grep -q "챌린지 존: $CHALLENGE_ZONE" || { echo "llms.txt missing zone" >&2; exit 1; }
echo "$llms" | grep -q "$BASE/api/domains" || { echo "llms.txt missing base url" >&2; exit 1; }
curl -fsS "$BASE/api/codes" | grep -q '"code":"CONNECT_FAILED"' || { echo "api/codes missing codes" >&2; exit 1; }

$COMPOSE ps worker --format '{{.State}}' | grep -q running || { $COMPOSE logs worker; echo "worker not running" >&2; exit 1; }
echo "smoke OK"
