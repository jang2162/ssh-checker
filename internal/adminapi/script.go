package adminapi

import (
	"fmt"
	"net/http"
	"strings"

	"ssl-checker/internal/httpx"
)

// webServer는 인증서를 받을 서버에서 쓰는 웹서버별 차이다. 스크립트가 이 표로 웹서버를 감지하고 설치 위치를 확인한다.
type webServer struct {
	ID    string
	Label string
	// Bins는 실행 파일 이름 후보다. 하나라도 찾으면 설치된 것으로 본다.
	Bins []string
	// ConfDirs는 설정 디렉터리 후보다. 환경변수 WEB_CONF_DIR로 직접 정할 수 있다.
	ConfDirs []string
	// ReloadDefault는 RELOAD_CMD를 비웠을 때 쓰는 명령이다. 셸이 변수를 풀어서 쓴다. root이면 SUDO_CMD가 비어 sudo가 빠진다.
	ReloadDefault string
	// Group이면 키 파일을 읽을 그룹을 묻는다(caddy는 일반 사용자로 돈다).
	Group bool
	// Config는 웹서버 설정 예다. %[1]s는 인증서 디렉터리로 바뀐다.
	Config string
}

var webServers = []webServer{
	{
		ID: "nginx", Label: "nginx",
		Bins:          []string{"nginx"},
		ConfDirs:      []string{"/etc/nginx", "/usr/local/etc/nginx", "/opt/homebrew/etc/nginx"},
		ReloadDefault: "${SUDO_CMD:+$SUDO_CMD }systemctl reload nginx",
		Config:        "ssl_certificate     %[1]s/fullchain.pem;\nssl_certificate_key %[1]s/key.pem;\n",
	},
	{
		ID: "apache", Label: "Apache",
		// Debian 계열은 apache2, RHEL 계열은 httpd다.
		Bins:          []string{"apache2", "httpd", "apachectl"},
		ConfDirs:      []string{"/etc/apache2", "/etc/httpd", "/usr/local/etc/httpd", "/opt/homebrew/etc/httpd"},
		ReloadDefault: "${SUDO_CMD:+$SUDO_CMD }systemctl reload apache2 || ${SUDO_CMD:+$SUDO_CMD }systemctl reload httpd",
		Config:        "SSLCertificateFile    %[1]s/fullchain.pem\nSSLCertificateKeyFile %[1]s/key.pem\n",
	},
	{
		ID: "caddy", Label: "Caddy",
		Bins:     []string{"caddy"},
		ConfDirs: []string{"/etc/caddy", "/usr/local/etc/caddy", "/opt/homebrew/etc/caddy"},
		// caddy는 키 파일을 읽을 수 있어야 해서, 그룹을 맞춘 뒤 reload한다.
		ReloadDefault: "${SUDO_CMD:+$SUDO_CMD }chgrp $CERT_GROUP $CERT_DIR/key.pem && chmod 640 $CERT_DIR/key.pem && ${SUDO_CMD:+$SUDO_CMD }systemctl reload caddy",
		Group:         true,
		Config:        "example.com, *.example.com {\n\ttls %[1]s/fullchain.pem %[1]s/key.pem\n}\n",
	},
}

// askFunc는 값을 물어보는 셸 함수다. 환경변수로 미리 받은 값은 묻지 않아서 에이전트와 자동화도 쓸 수 있다.
// 비밀번호는 화면에 보이지 않게 받고, 파일이나 셸 기록에 남지 않는다.
const askFunc = `die() { echo "$*" >&2; exit 1; }

# 입력을 받을 터미널을 출력한다. 없으면 실패한다. curl ... | bash처럼 표준입력이 파이프여도 /dev/tty로 받는다.
tty_in() {
  if [ -t 0 ]; then echo /dev/stdin; elif { : </dev/tty; } 2>/dev/null; then echo /dev/tty; else return 1; fi
}

# ask 변수이름 "설명" [기본값] [secret]
ask() {
  local var=$1 label=$2 def=${3:-} secret=${4:-} val="" in=""
  if [ -n "${!var:-}" ]; then return; fi
  in=$(tty_in) || in=""
  if [ -n "$in" ]; then
    if [ -n "$secret" ]; then
      read -rsp "$label: " val <"$in"; echo
    else
      read -rp "$label${def:+ [$def]}: " val <"$in"
    fi
  fi
  # 터미널이 없으면(에이전트, 자동화) 기본값을 쓴다. 기본값이 없는 값은 환경변수로 넘겨야 한다.
  val=${val:-$def}
  [ -n "$val" ] || die "$var 값이 필요합니다. 터미널에서 실행하거나 환경변수로 넘기세요."
  printf -v "$var" '%s' "$val"
  export "$var"
}
`

// findBinFunc는 실행 파일을 찾는다. 일반 사용자의 PATH에는 /usr/sbin이 없는 경우가 많아서 흔한 위치도 본다.
// 더 볼 디렉터리는 WEB_SEARCH_DIRS(공백으로 구분)로 바꿀 수 있다.
const findBinFunc = `find_bin() {
  local n p
  for n in "$@"; do
    command -v "$n" 2>/dev/null && return 0
    for p in ${WEB_SEARCH_DIRS:-/usr/sbin /usr/local/sbin /sbin /usr/local/bin /opt/homebrew/bin}; do
      if [ -x "$p/$n" ]; then echo "$p/$n"; return 0; fi
    done
  done
  return 1
}
`

// acmeFuncs는 acme.sh 설치와 버전(3.1.4 이상, dns_acmedns 서브도메인 지원)을 확인하는 셸 함수다.
const acmeFuncs = `ACME_SH="$HOME/.acme.sh/acme.sh"
acme_version() { [ -x "$ACME_SH" ] && "$ACME_SH" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | tail -1; }
acme_ok() {
  local v; v=$(acme_version || true)
  [ -n "$v" ] && [ "$(printf '%s\n3.1.4\n' "$v" | sort -V | head -1)" = "3.1.4" ]
}
`

// verifyFunc는 입력한 자격 증명으로 ssl-checker의 /verify를 불러, 자격 증명과 이 서버의 IP가 허용 목록에 있는지 확인한다.
// 비밀번호는 curl 인자가 아니라 표준입력 설정(-K -)으로 넘겨 프로세스 목록에 보이지 않게 한다.
const verifyFunc = `verify_server() {
  local tmp code ip sub
  tmp=$(mktemp)
  code=$(printf 'header = "X-Api-User: %s"\nheader = "X-Api-Key: %s"\n' "$ACMEDNS_USERNAME" "$ACMEDNS_PASSWORD" |
    curl -sS -K - -o "$tmp" -w '%{http_code}' -X POST "$ACMEDNS_BASE_URL/verify") ||
    { rm -f "$tmp"; die "ssl-checker($ACMEDNS_BASE_URL)에 연결하지 못했습니다. 주소와 네트워크를 확인하세요."; }
  ip=$(sed -n 's/.*"source_ip":"\([^"]*\)".*/\1/p' "$tmp")
  FULLDOMAIN=$(sed -n 's/.*"fulldomain":"\([^"]*\)".*/\1/p' "$tmp")
  sub=$(sed -n 's/.*"subdomain":"\([^"]*\)".*/\1/p' "$tmp")
  rm -f "$tmp"
  case "$code" in
    200)
      # subdomain은 계정에 묶여 있어 서버가 알려 준다. acme.sh(dns_acmedns)가 쓰는 값이라 입력받지 않는다.
      [ -z "$sub" ] || { ACMEDNS_SUBDOMAIN=$sub; export ACMEDNS_SUBDOMAIN; }
      [ -n "${ACMEDNS_SUBDOMAIN:-}" ] || die "서버가 subdomain을 알려 주지 않았습니다. ACMEDNS_SUBDOMAIN으로 직접 넘기세요."
      echo "확인됨: 자격 증명이 맞고, 이 서버의 IP(${ip:-알 수 없음})가 허용 목록에 있습니다." ;;
    401) die "username 또는 password가 맞지 않습니다." ;;
    403) die "이 서버의 IP(${ip:-알 수 없음})가 허용 IP 목록에 없습니다. 관리자에게 이 IP를 허용 목록에 추가해 달라고 요청하세요." ;;
    429) die "인증 실패가 너무 많아 잠시 차단됐습니다. 잠시 후 다시 시도하세요." ;;
    *) die "자격 증명 확인에 실패했습니다(HTTP $code)." ;;
  esac
}
`

// cnameFuncs는 도메인의 _acme-challenge CNAME이 ssl-checker가 알려 준 주소(FULLDOMAIN)를 가리키는지 확인한다.
// Let's Encrypt가 보는 것과 같게 공개 리졸버(기본 1.1.1.1)로 조회하고, 조회 도구(dig, host, nslookup)가 없으면 DNS over HTTPS로 조회한다.
const cnameFuncs = `lookup_cname() {
  local name=$1 out="" used="" tool=${CNAME_TOOL:-auto} r=${DNS_RESOLVER:-1.1.1.1}
  r=${r%%:*}
  if [ -z "$used" ] && [ "$tool" = auto -o "$tool" = dig ] && command -v dig >/dev/null; then
    out=$(dig +short +time=5 +tries=1 CNAME "$name" @"$r" 2>/dev/null | head -1); used=1
  fi
  if [ -z "$used" ] && [ "$tool" = auto -o "$tool" = host ] && command -v host >/dev/null; then
    out=$(host -t CNAME "$name" "$r" 2>/dev/null | sed -n 's/.* is an alias for \(.*\)/\1/p' | head -1); used=1
  fi
  if [ -z "$used" ] && [ "$tool" = auto -o "$tool" = nslookup ] && command -v nslookup >/dev/null; then
    out=$(nslookup -type=CNAME "$name" "$r" 2>/dev/null | sed -n 's/.*canonical name = \(.*\)/\1/p' | head -1); used=1
  fi
  if [ -z "$used" ]; then
    out=$(curl -fsS -m 10 -H 'accept: application/dns-json' "https://cloudflare-dns.com/dns-query?name=$name&type=CNAME" 2>/dev/null |
      sed -n 's/.*"type":5,"TTL":[0-9]*,"data":"\([^"]*\)".*/\1/p' | head -1)
  fi
  out=$(printf '%s' "$out" | tr 'A-Z' 'a-z'); echo "${out%.}"
}

# check_cname 도메인 — CNAME이 맞을 때까지 확인한다. 터미널이면 등록한 뒤 다시 확인할 수 있고, 아니면 멈춘다.
check_cname() {
  local name="_acme-challenge.$1" want got in ans
  want=$(printf '%s' "$FULLDOMAIN" | tr 'A-Z' 'a-z'); want=${want%.}
  [ -n "$want" ] || { echo "알림: 서버가 챌린지 주소를 알려 주지 않아 CNAME 확인을 건너뜁니다."; return 0; }
  while :; do
    got=$(lookup_cname "$name")
    if [ "$got" = "$want" ]; then echo "확인됨: $name CNAME → $want"; return 0; fi
    if [ -z "$got" ]; then echo "CNAME이 없습니다: $name" >&2; else echo "CNAME이 다른 곳을 가리킵니다: $name → $got" >&2; fi
    echo "  DNS에 등록하세요: $name.  CNAME  $want." >&2
    if [ -n "${SKIP_CNAME_CHECK:-}" ]; then echo "SKIP_CNAME_CHECK가 설정되어 확인을 건너뜁니다. 발급은 CNAME이 전파되어야 성공합니다." >&2; return 0; fi
    in=$(tty_in) || die "등록한 뒤 다시 실행하세요 (DNS 전파에 몇 분 걸릴 수 있습니다. 확인을 건너뛰려면 SKIP_CNAME_CHECK=1)."
    read -rp "등록했으면 Enter로 다시 확인합니다 (s 입력 시 건너뜀): " ans <"$in"
    [ "$ans" = s ] && return 0
  done
}
`

// setupScript는 인증서를 받을 서버에서 내려받아 실행하는 스크립트 하나다. 자격 증명·허용 IP 확인, acme.sh 설치 확인(없으면 설치),
// 웹서버 감지와 설치 위치 확인, 발급·설치, 웹서버 설정 반영 확인까지 한다(acme.sh 3.1.4 이상).
// 자격 증명은 스크립트에 넣지 않고 실행할 때 입력받는다. baseURL은 내려받은 주소라서 기본값으로만 쓴다.
func setupScript(baseURL string) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n# 인증서 발급·설치. 실행하면 값을 물어본다.\n")
	b.WriteString("# 환경변수로 미리 넘기면 묻지 않는다. 키를 재발급한 뒤에는 FORCE=1로 실행한다.\n")
	b.WriteString("set -eu\n\n" + askFunc + "\n" + findBinFunc + "\n" + acmeFuncs + "\n" + verifyFunc + "\n" + cnameFuncs + "\n")
	b.WriteString("command -v curl >/dev/null || die \"curl이 필요합니다. 먼저 설치하세요.\"\n\n")
	b.WriteString("ask SSLCHK_URL \"ssl-checker 주소\"")
	if baseURL != "" {
		fmt.Fprintf(&b, " %q", baseURL)
	}
	b.WriteString("\nSSLCHK_URL=${SSLCHK_URL%/}\n")
	b.WriteString("# acme.sh의 dns_acmedns가 부르는 주소. 인증서를 받는 서버용 API와 스크립트는 /acme/ 아래에 있다.\n")
	b.WriteString("ACMEDNS_BASE_URL=\"${ACMEDNS_BASE_URL:-$SSLCHK_URL/acme}\"; export ACMEDNS_BASE_URL\n")
	// root이면 sudo가 필요 없다(설치되어 있지 않을 수도 있다). 일반 계정이면 sudo를 쓴다. 환경변수 SUDO_CMD로 바꿀 수 있다.
	b.WriteString("\nif [ -z \"${SUDO_CMD+x}\" ]; then if [ \"$(id -u)\" = 0 ]; then SUDO_CMD=\"\"; else SUDO_CMD=sudo; fi; fi\n")
	b.WriteString("\n# 1) 자격 증명을 입력받고, 이 서버의 IP가 허용 목록에 있는지 확인한다. 통과해야 아래에서 설치한다.\n")
	b.WriteString("ask ACMEDNS_USERNAME \"username\"\n")
	b.WriteString("ask ACMEDNS_PASSWORD \"password (입력은 보이지 않습니다)\" \"\" secret\n")
	b.WriteString("verify_server\n\n")
	b.WriteString("# 2) acme.sh가 3.1.4 이상으로 설치되어 있는지 확인하고, 없거나 낮으면 설치한다.\n")
	b.WriteString("if acme_ok; then\n")
	b.WriteString("  echo \"확인됨: acme.sh $(acme_version)\"\n")
	b.WriteString("else\n")
	b.WriteString("  if [ -x \"$ACME_SH\" ]; then echo \"acme.sh가 3.1.4보다 낮습니다. 업그레이드합니다.\"; else echo \"acme.sh가 없습니다. 설치합니다.\"; fi\n")
	b.WriteString("  ask ACME_EMAIL \"인증서 만료 알림을 받을 메일 주소\"\n")
	b.WriteString("  # cron이 있어야 자동 갱신이 등록된다.\n")
	b.WriteString("  curl -fsSL https://get.acme.sh | sh -s email=\"$ACME_EMAIL\"\n")
	b.WriteString("  acme_ok || die \"설치 뒤에도 acme.sh 3.1.4 이상을 확인하지 못했습니다.\"\n")
	b.WriteString("  echo \"acme.sh $(acme_version) 설치 완료\"\n")
	b.WriteString("fi\n\n")
	b.WriteString("# 3) 웹서버를 정하고, 설치 위치를 확인한다. 설치된 웹서버를 찾아 기본값으로 제안한다.\n")
	b.WriteString("DETECTED=\"\"\n") // set -u에서 감지된 것이 없을 때도 안전하도록 먼저 비워 둔다
	ids := make([]string, 0, len(webServers))
	for _, ws := range webServers {
		ids = append(ids, ws.ID)
		fmt.Fprintf(&b, "if find_bin %s >/dev/null; then DETECTED=\"$DETECTED %s\"; fi\n", strings.Join(ws.Bins, " "), ws.ID)
	}
	b.WriteString("DETECTED=${DETECTED# }\n")
	b.WriteString("if [ -n \"$DETECTED\" ]; then echo \"감지된 웹서버: $DETECTED\"; else echo \"감지된 웹서버가 없습니다. 수동 설정(manual)으로 진행합니다: reload 스크립트를 직접 입력하세요.\"; fi\n")
	b.WriteString("WEB_DEFAULT=${DETECTED%% *}\n")
	fmt.Fprintf(&b, "ask WEB_SERVER \"웹서버 (%s, manual=수동)\" \"${WEB_DEFAULT:-manual}\"\n", strings.Join(ids, ", "))
	b.WriteString("case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "  %s) WEB_BINS=\"%s\"; WEB_CONF_DIRS=\"%s\" ;;\n", ws.ID, strings.Join(ws.Bins, " "), strings.Join(ws.ConfDirs, " "))
	}
	// 수동 설정: 웹서버를 확인하지 않고 reload 스크립트를 직접 받는다.
	b.WriteString("  manual) WEB_BINS=\"\"; WEB_CONF_DIRS=\"\"; SKIP_WEB_CHECK=1 ;;\n")
	fmt.Fprintf(&b, "  *) die \"WEB_SERVER는 %s, manual 중 하나여야 합니다.\" ;;\nesac\n", strings.Join(ids, ", "))
	b.WriteString("# 실행 파일이 없으면 reload가 실패하므로 발급 전에 멈춘다. 건너뛰려면 SKIP_WEB_CHECK=1.\n")
	b.WriteString("if [ -z \"${SKIP_WEB_CHECK:-}\" ]; then\n")
	b.WriteString("  WEB_BIN=$(find_bin $WEB_BINS) || die \"$WEB_SERVER 실행 파일을 찾지 못했습니다. 설치되어 있는지, 다른 웹서버를 쓰거나 직접 reload하려면 WEB_SERVER=manual로 실행하세요 (확인만 건너뛰려면 SKIP_WEB_CHECK=1).\"\n")
	b.WriteString("  echo \"확인됨: $WEB_SERVER 실행 파일 $WEB_BIN\"\n")
	b.WriteString("fi\n")
	b.WriteString("WEB_CONF=\"\"\n")
	b.WriteString("for d in ${WEB_CONF_DIR:-$WEB_CONF_DIRS}; do if [ -d \"$d\" ]; then WEB_CONF=$d; break; fi; done\n")
	b.WriteString("if [ -n \"$WEB_CONF\" ]; then echo \"확인됨: $WEB_SERVER 설정 디렉터리 $WEB_CONF\"; else echo \"알림: $WEB_SERVER 설정 디렉터리를 찾지 못했습니다. 설정 반영 확인은 건너뜁니다 (위치를 알면 WEB_CONF_DIR로 넘기세요).\"; fi\n\n")
	b.WriteString("# 4) 인증서를 저장할 도메인과 경로를 정한다.\n")
	b.WriteString("ask DOMAIN \"인증서를 받을 도메인 (예: example.com, *.example.com도 함께 발급)\"\n")
	// 와일드카드는 항상 함께 발급한다. *.example.com을 입력했다면 example.com으로 본다(그대로 쓰면 **.example.com이 된다).
	b.WriteString("if [ \"${DOMAIN#\\*.}\" != \"$DOMAIN\" ]; then DOMAIN=${DOMAIN#\\*.}; echo \"알림: *.는 빼고 $DOMAIN으로 진행합니다. 와일드카드(*.$DOMAIN)는 함께 발급됩니다.\"; fi\n")
	b.WriteString("# 도메인의 _acme-challenge CNAME이 ssl-checker가 알려 준 주소를 가리키는지 확인한다. 없으면 발급이 실패하므로 미리 알려 준다.\n")
	b.WriteString("check_cname \"$DOMAIN\"\n")
	b.WriteString("ask CERT_DIR \"인증서를 저장할 디렉터리\" \"/etc/ssl/$DOMAIN\"\n")
	// 웹서버마다 다른 값: 키 파일 그룹(caddy)과 reload 기본 명령. 변수를 다 받은 뒤에 풀어서 기본값으로 쓴다.
	b.WriteString("case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "  %s)\n", ws.ID)
		if ws.Group {
			b.WriteString("    ask CERT_GROUP \"키 파일을 읽을 그룹\" \"caddy\"\n")
		}
		fmt.Fprintf(&b, "    RELOAD_DEFAULT=\"%s\" ;;\n", ws.ReloadDefault)
	}
	b.WriteString("  manual)\n    echo \"수동 설정: 인증서를 설치한 뒤 웹서버를 다시 읽게 하는 reload 스크립트나 명령을 입력하세요. 예: /usr/local/bin/reload-web.sh, systemctl reload haproxy\"\n    RELOAD_DEFAULT=\"\" ;;\n")
	b.WriteString("esac\n")
	b.WriteString("ask RELOAD_CMD \"설치 뒤 실행할 reload 스크립트 또는 명령\" \"$RELOAD_DEFAULT\"\n")
	// 스크립트 경로(/로 시작)를 넘겼다면 지금 실행할 수 있는지 확인한다. sudo는 건너뛰고 본다.
	b.WriteString("RELOAD_BIN=${RELOAD_CMD#sudo }; RELOAD_BIN=${RELOAD_BIN%% *}\n")
	b.WriteString("case \"$RELOAD_BIN\" in /*|./*) [ -x \"$RELOAD_BIN\" ] || die \"reload 스크립트를 실행할 수 없습니다: $RELOAD_BIN (파일이 있는지, 실행 권한이 있는지 확인하세요)\" ;; esac\n\n")
	b.WriteString("# 5) 인증서를 발급해 설치한다.\n")
	b.WriteString("\"$ACME_SH\" --set-default-ca --server letsencrypt\n")
	b.WriteString("# 처음 연결하는 도메인은 --issue에 --staging을 붙여 먼저 확인한다(docs 참고).\n")
	b.WriteString("# FORCE=1이면 이미 인증서가 있어도 다시 발급해, 새 키를 도메인 설정에 저장한다.\n")
	// 종료 코드 2는 "아직 갱신할 때가 아님"이다. 스크립트를 다시 돌려도 멈추지 않게 한다.
	b.WriteString(`"$ACME_SH" --issue --keylength ec-256 --dns dns_acmedns -d "$DOMAIN" -d "*.$DOMAIN" ${FORCE:+--force} || [ "$?" -eq 2 ]` + "\n")
	// 저장 디렉터리는 sudo 없이 만들거나 쓸 수 있으면 그대로 쓴다. 안 될 때만 sudo로 만들고 소유자를 현재 계정으로 맞춘다
	// (자동 갱신이 같은 계정으로 파일을 쓰기 때문이다).
	b.WriteString("if ! { mkdir -p \"$CERT_DIR\" 2>/dev/null && [ -w \"$CERT_DIR\" ]; }; then\n")
	b.WriteString("  echo \"인증서 저장 디렉터리($CERT_DIR)를 만들려면 sudo가 필요합니다. 비밀번호를 물으면 입력하세요.\"\n")
	b.WriteString("  ${SUDO_CMD:+$SUDO_CMD }install -d -o \"$(id -un)\" \"$CERT_DIR\"\n")
	b.WriteString("fi\n")
	b.WriteString("\"$ACME_SH\" --install-cert -d \"$DOMAIN\" --ecc \\\n")
	b.WriteString("  --key-file \"$CERT_DIR/key.pem\" \\\n")
	b.WriteString("  --fullchain-file \"$CERT_DIR/fullchain.pem\" \\\n")
	b.WriteString("  --reloadcmd \"$RELOAD_CMD\"\n\n")
	b.WriteString("# 6) 웹서버 설정이 이 인증서를 쓰는지 확인하고, 아니면 넣을 설정을 보여 준다.\n")
	b.WriteString("echo \"인증서를 저장했습니다: $CERT_DIR/fullchain.pem, $CERT_DIR/key.pem\"\n")
	b.WriteString("if [ -n \"$WEB_CONF\" ] && { grep -rqsF -- \"$CERT_DIR/fullchain.pem\" \"$WEB_CONF\" 2>/dev/null || { [ -n \"$SUDO_CMD\" ] && $SUDO_CMD -n grep -rqsF -- \"$CERT_DIR/fullchain.pem\" \"$WEB_CONF\" 2>/dev/null; }; }; then\n")
	b.WriteString("  echo \"확인됨: $WEB_SERVER 설정이 이미 이 인증서를 쓰고 있습니다.\"\n")
	b.WriteString("else\n")
	b.WriteString("  echo \"$WEB_SERVER 설정에 아래를 넣고 reload하세요:\"\n")
	b.WriteString("  case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "    %s)\n      cat <<EOF_CONFIG\n%sEOF_CONFIG\n      ;;\n", ws.ID, fmt.Sprintf(ws.Config, "$CERT_DIR"))
	}
	b.WriteString("    manual)\n      echo \"  인증서(체인 포함): $CERT_DIR/fullchain.pem\"\n      echo \"  개인 키:          $CERT_DIR/key.pem\"\n      ;;\n")
	b.WriteString("  esac\nfi\n\n")
	b.WriteString("# 7) 자동 갱신(cron)은 비밀번호를 입력할 수 없다. reload 명령이 sudo를 쓰는데 비밀번호가 필요하면 갱신 때 reload가 실패한다.\n")
	b.WriteString("case \"$RELOAD_CMD\" in\n  *sudo*)\n")
	b.WriteString("    sudo -k 2>/dev/null || true # 방금 입력한 비밀번호의 캐시를 지워야 정확히 알 수 있다\n")
	b.WriteString("    if ! sudo -n true 2>/dev/null; then\n")
	b.WriteString("      echo \"\" >&2\n")
	b.WriteString("      echo \"경고: reload 명령이 sudo를 쓰는데, 이 계정은 sudo에 비밀번호가 필요합니다.\" >&2\n")
	b.WriteString("      echo \"  자동 갱신(cron)은 비밀번호를 입력할 수 없어서, 인증서 파일은 갱신돼도 웹서버가 reload되지 않고 옛 인증서로 계속 서비스됩니다.\" >&2\n")
	b.WriteString("      echo \"  둘 중 하나로 해결하세요:\" >&2\n")
	b.WriteString("      echo \"  1) reload에 필요한 명령만 비밀번호 없이 허용합니다 (sudoers의 NOPASSWD, visudo로 편집).\" >&2\n")
	b.WriteString("      echo \"  2) sudo 없이 실행되는 reload 스크립트를 RELOAD_CMD로 지정합니다 (예: docker 그룹이면 docker exec <컨테이너> ... reload).\" >&2\n")
	b.WriteString("      echo \"  reload 명령을 바꾸려면 ~/.acme.sh/acme.sh --install-cert -d $DOMAIN --ecc --reloadcmd '<새 명령>'을 다시 실행하세요.\" >&2\n")
	b.WriteString("    fi ;;\nesac\n")
	return b.String()
}

// scriptView는 스크립트 하나의 내려받기 주소, 실행 명령, 내용이다. 비밀값은 들어 있지 않다.
type scriptView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Cmd    string `json:"cmd"`
	Script string `json:"script"`
}

// serverView는 웹서버 하나의 설정 예다. 설치·세팅 스크립트는 하나이고, 웹서버는 스크립트가 감지하거나 묻는다.
type serverView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Config string `json:"config"`
}

// setupInfo는 설치·세팅 스크립트와 웹서버별 실행 명령이다.
type setupInfo struct {
	Setup scriptView `json:"setup"`
	// CertDir는 서버 설정 예(Config)에 쓰인 인증서 디렉터리다. 화면이 사용자가 입력한 경로로 바꿔 보여 줄 때 이 값을 치환한다.
	CertDir string       `json:"cert_dir"`
	Servers []serverView `json:"servers"`
}

// 스크립트는 인증서를 받는 서버용 API와 같은 접두사(/acme/) 아래에 둔다. 프록시에서 /acme/* 한 줄로 열 수 있다.
const setupFile = "setup.sh"

// buildSetup은 base 주소 기준의 내려받기 주소와 실행 명령을 만든다. dir은 설정 예에 쓸 인증서 디렉터리다.
func buildSetup(base, dir string) setupInfo {
	download := func(file string) (url, cmd string) {
		url = base + "/acme/" + file
		return url, fmt.Sprintf("curl -fsSL %s -o %s && ", url, file)
	}
	su, sc := download(setupFile)
	info := setupInfo{
		Setup: scriptView{ID: "setup", Label: "세팅", URL: su, Cmd: sc + "bash " + setupFile, Script: setupScript(base)},
	}
	for _, ws := range webServers {
		info.Servers = append(info.Servers, serverView{ID: ws.ID, Label: ws.Label, Config: fmt.Sprintf(ws.Config, dir)})
	}
	return info
}

// setupBase는 안내에 쓸 ssl-checker 주소다. 알 수 없으면 자리표시자를 쓴다.
func (a *API) setupBase(r *http.Request) string {
	if base := a.PublicURLFor(r); base != "" {
		return base
	}
	return "<ssl-checker 주소>"
}

// setup은 스크립트의 내용과 실행 명령이다. 누구나 볼 수 있다.
func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, buildSetup(a.setupBase(r), "<CERT_DIR>"))
}

// scriptFile은 스크립트를 텍스트로 내려준다. 서버에서 curl로 받아 실행한다.
func scriptFile(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

// SetupScript는 GET /acme/setup.sh다. 내려받은 주소(PUBLIC_URL, 없으면 요청 주소)를 주소 기본값으로 넣는다.
func (a *API) SetupScript(w http.ResponseWriter, r *http.Request) {
	scriptFile(w, setupScript(a.PublicURLFor(r)))
}
