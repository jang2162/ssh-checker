package adminapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/lang"
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

@@c_tty@@
tty_in() {
  if [ -t 0 ]; then echo /dev/stdin; elif { : </dev/tty; } 2>/dev/null; then echo /dev/tty; else return 1; fi
}

@@c_ask@@
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
  @@c_default@@
  val=${val:-$def}
  [ -n "$val" ] || die "@@e_value@@"
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
    { rm -f "$tmp"; die "@@e_connect@@"; }
  ip=$(sed -n 's/.*"source_ip":"\([^"]*\)".*/\1/p' "$tmp")
  FULLDOMAIN=$(sed -n 's/.*"fulldomain":"\([^"]*\)".*/\1/p' "$tmp")
  sub=$(sed -n 's/.*"subdomain":"\([^"]*\)".*/\1/p' "$tmp")
  rm -f "$tmp"
  case "$code" in
    200)
      @@c_subdomain@@
      [ -z "$sub" ] || { ACMEDNS_SUBDOMAIN=$sub; export ACMEDNS_SUBDOMAIN; }
      [ -n "${ACMEDNS_SUBDOMAIN:-}" ] || die "@@e_subdomain@@"
      echo "@@i_verified@@" ;;
    401) die "@@e_cred@@" ;;
    403) die "@@e_ip@@" ;;
    429) die "@@e_blocked@@" ;;
    *) die "@@e_verify@@" ;;
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

@@c_check_cname@@
check_cname() {
  local name="_acme-challenge.$1" want got in ans
  want=$(printf '%s' "$FULLDOMAIN" | tr 'A-Z' 'a-z'); want=${want%.}
  [ -n "$want" ] || { echo "@@n_cname_skip@@"; return 0; }
  while :; do
    got=$(lookup_cname "$name")
    if [ "$got" = "$want" ]; then echo "@@i_cname_ok@@"; return 0; fi
    if [ -z "$got" ]; then echo "@@e_cname_none@@" >&2; else echo "@@e_cname_wrong@@" >&2; fi
    echo "@@i_cname_register@@" >&2
    if [ -n "${SKIP_CNAME_CHECK:-}" ]; then echo "@@n_cname_skipped@@" >&2; return 0; fi
    in=$(tty_in) || die "@@e_cname_rerun@@"
    read -rp "@@p_cname_retry@@" ans <"$in"
    [ "$ans" = s ] && return 0
  done
}
`

// setupScript는 인증서를 받을 서버에서 내려받아 실행하는 스크립트 하나다. 자격 증명·허용 IP 확인, acme.sh 설치 확인(없으면 설치),
// 웹서버 감지와 설치 위치 확인, 발급·설치, 웹서버 설정 반영 확인까지 한다(acme.sh 3.1.4 이상).
// 자격 증명은 스크립트에 넣지 않고 실행할 때 입력받는다. baseURL은 내려받은 주소라서 기본값으로만 쓴다.
func setupScript(baseURL string, l lang.Lang) string { return expand(setupScriptTokens(baseURL), l) }

// setupScriptTokens는 @@키@@가 남아 있는 스크립트 본문이다. 글은 언어별 표(scriptMessages)에서 채운다.
func setupScriptTokens(baseURL string) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n@@c_header1@@\n")
	b.WriteString("@@c_header2@@\n")
	b.WriteString("set -eu\n\n" + askFunc + "\n" + findBinFunc + "\n" + acmeFuncs + "\n" + verifyFunc + "\n" + cnameFuncs + "\n")
	b.WriteString("command -v curl >/dev/null || die \"@@e_curl@@\"\n\n")
	b.WriteString("ask SSLCHK_URL \"@@p_url@@\"")
	if baseURL != "" {
		fmt.Fprintf(&b, " %q", baseURL)
	}
	b.WriteString("\nSSLCHK_URL=${SSLCHK_URL%/}\n")
	b.WriteString("@@c_baseurl@@\n")
	b.WriteString("ACMEDNS_BASE_URL=\"${ACMEDNS_BASE_URL:-$SSLCHK_URL/acme}\"; export ACMEDNS_BASE_URL\n")
	// root이면 sudo가 필요 없다(설치되어 있지 않을 수도 있다). 일반 계정이면 sudo를 쓴다. 환경변수 SUDO_CMD로 바꿀 수 있다.
	b.WriteString("\nif [ -z \"${SUDO_CMD+x}\" ]; then if [ \"$(id -u)\" = 0 ]; then SUDO_CMD=\"\"; else SUDO_CMD=sudo; fi; fi\n")
	b.WriteString("\n@@c_step1@@\n")
	b.WriteString("ask ACMEDNS_USERNAME \"username\"\n")
	b.WriteString("ask ACMEDNS_PASSWORD \"@@p_password@@\" \"\" secret\n")
	b.WriteString("verify_server\n\n")
	b.WriteString("@@c_step2@@\n")
	b.WriteString("if acme_ok; then\n")
	b.WriteString("  echo \"@@i_acme_ok@@\"\n")
	b.WriteString("else\n")
	b.WriteString("  if [ -x \"$ACME_SH\" ]; then echo \"@@n_acme_old@@\"; else echo \"@@n_acme_missing@@\"; fi\n")
	b.WriteString("  ask ACME_EMAIL \"@@p_email@@\"\n")
	b.WriteString("  @@c_cron@@\n")
	b.WriteString("  curl -fsSL https://get.acme.sh | sh -s email=\"$ACME_EMAIL\"\n")
	b.WriteString("  acme_ok || die \"@@e_acme_after@@\"\n")
	b.WriteString("  echo \"@@i_acme_done@@\"\n")
	b.WriteString("fi\n\n")
	b.WriteString("@@c_step3@@\n")
	b.WriteString("DETECTED=\"\"\n") // set -u에서 감지된 것이 없을 때도 안전하도록 먼저 비워 둔다
	for _, ws := range webServers {
		fmt.Fprintf(&b, "if find_bin %s >/dev/null; then DETECTED=\"$DETECTED %s\"; fi\n", strings.Join(ws.Bins, " "), ws.ID)
	}
	b.WriteString("DETECTED=${DETECTED# }\n")
	b.WriteString("if [ -n \"$DETECTED\" ]; then echo \"@@i_detected@@\"; else echo \"@@n_detected_none@@\"; fi\n")
	b.WriteString("WEB_DEFAULT=${DETECTED%% *}\n")
	b.WriteString("ask WEB_SERVER \"@@p_web@@\" \"${WEB_DEFAULT:-manual}\"\n")
	b.WriteString("case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "  %s) WEB_BINS=\"%s\"; WEB_CONF_DIRS=\"%s\" ;;\n", ws.ID, strings.Join(ws.Bins, " "), strings.Join(ws.ConfDirs, " "))
	}
	// 수동 설정: 웹서버를 확인하지 않고 reload 스크립트를 직접 받는다.
	b.WriteString("  manual) WEB_BINS=\"\"; WEB_CONF_DIRS=\"\"; SKIP_WEB_CHECK=1 ;;\n")
	b.WriteString("  *) die \"@@e_web_invalid@@\" ;;\nesac\n")
	b.WriteString("@@c_webcheck@@\n")
	b.WriteString("if [ -z \"${SKIP_WEB_CHECK:-}\" ]; then\n")
	b.WriteString("  WEB_BIN=$(find_bin $WEB_BINS) || die \"@@e_web_missing@@\"\n")
	b.WriteString("  echo \"@@i_web_bin@@\"\n")
	b.WriteString("fi\n")
	b.WriteString("WEB_CONF=\"\"\n")
	b.WriteString("for d in ${WEB_CONF_DIR:-$WEB_CONF_DIRS}; do if [ -d \"$d\" ]; then WEB_CONF=$d; break; fi; done\n")
	b.WriteString("if [ -n \"$WEB_CONF\" ]; then echo \"@@i_web_conf@@\"; else echo \"@@n_web_conf_none@@\"; fi\n\n")
	b.WriteString("@@c_step4@@\n")
	b.WriteString("ask DOMAIN \"@@p_domain@@\"\n")
	// 와일드카드는 항상 함께 발급한다. *.example.com을 입력했다면 example.com으로 본다(그대로 쓰면 **.example.com이 된다).
	b.WriteString("if [ \"${DOMAIN#\\*.}\" != \"$DOMAIN\" ]; then DOMAIN=${DOMAIN#\\*.}; echo \"@@n_wildcard@@\"; fi\n")
	b.WriteString("@@c_cname@@\n")
	b.WriteString("check_cname \"$DOMAIN\"\n")
	b.WriteString("ask CERT_DIR \"@@p_certdir@@\" \"/etc/ssl/$DOMAIN\"\n")
	// 웹서버마다 다른 값: 키 파일 그룹(caddy)과 reload 기본 명령. 변수를 다 받은 뒤에 풀어서 기본값으로 쓴다.
	b.WriteString("case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "  %s)\n", ws.ID)
		if ws.Group {
			b.WriteString("    ask CERT_GROUP \"@@p_group@@\" \"caddy\"\n")
		}
		fmt.Fprintf(&b, "    RELOAD_DEFAULT=\"%s\" ;;\n", ws.ReloadDefault)
	}
	b.WriteString("  manual)\n    echo \"@@n_manual@@\"\n    RELOAD_DEFAULT=\"\" ;;\n")
	b.WriteString("esac\n")
	b.WriteString("ask RELOAD_CMD \"@@p_reload@@\" \"$RELOAD_DEFAULT\"\n")
	// 스크립트 경로(/로 시작)를 넘겼다면 지금 실행할 수 있는지 확인한다. sudo는 건너뛰고 본다.
	b.WriteString("RELOAD_BIN=${RELOAD_CMD#sudo }; RELOAD_BIN=${RELOAD_BIN%% *}\n")
	b.WriteString("case \"$RELOAD_BIN\" in /*|./*) [ -x \"$RELOAD_BIN\" ] || die \"@@e_reload@@\" ;; esac\n\n")
	b.WriteString("@@c_step5@@\n")
	b.WriteString("\"$ACME_SH\" --set-default-ca --server letsencrypt\n")
	b.WriteString("@@c_staging@@\n")
	b.WriteString("@@c_force@@\n")
	// 종료 코드 2는 "아직 갱신할 때가 아님"이다. 스크립트를 다시 돌려도 멈추지 않게 한다.
	b.WriteString(`"$ACME_SH" --issue --keylength ec-256 --dns dns_acmedns -d "$DOMAIN" -d "*.$DOMAIN" ${FORCE:+--force} || [ "$?" -eq 2 ]` + "\n")
	// 저장 디렉터리는 sudo 없이 만들거나 쓸 수 있으면 그대로 쓴다. 안 될 때만 sudo로 만들고 소유자를 현재 계정으로 맞춘다
	// (자동 갱신이 같은 계정으로 파일을 쓰기 때문이다).
	b.WriteString("if ! { mkdir -p \"$CERT_DIR\" 2>/dev/null && [ -w \"$CERT_DIR\" ]; }; then\n")
	b.WriteString("  echo \"@@n_sudo_dir@@\"\n")
	b.WriteString("  ${SUDO_CMD:+$SUDO_CMD }install -d -o \"$(id -un)\" \"$CERT_DIR\"\n")
	b.WriteString("fi\n")
	b.WriteString("\"$ACME_SH\" --install-cert -d \"$DOMAIN\" --ecc \\\n")
	b.WriteString("  --key-file \"$CERT_DIR/key.pem\" \\\n")
	b.WriteString("  --fullchain-file \"$CERT_DIR/fullchain.pem\" \\\n")
	b.WriteString("  --reloadcmd \"$RELOAD_CMD\"\n\n")
	b.WriteString("@@c_step6@@\n")
	b.WriteString("echo \"@@i_saved@@\"\n")
	b.WriteString("if [ -n \"$WEB_CONF\" ] && { grep -rqsF -- \"$CERT_DIR/fullchain.pem\" \"$WEB_CONF\" 2>/dev/null || { [ -n \"$SUDO_CMD\" ] && $SUDO_CMD -n grep -rqsF -- \"$CERT_DIR/fullchain.pem\" \"$WEB_CONF\" 2>/dev/null; }; }; then\n")
	b.WriteString("  echo \"@@i_web_uses@@\"\n")
	b.WriteString("else\n")
	b.WriteString("  echo \"@@i_web_add@@\"\n")
	b.WriteString("  case \"$WEB_SERVER\" in\n")
	for _, ws := range webServers {
		fmt.Fprintf(&b, "    %s)\n      cat <<EOF_CONFIG\n%sEOF_CONFIG\n      ;;\n", ws.ID, fmt.Sprintf(ws.Config, "$CERT_DIR"))
	}
	b.WriteString("    manual)\n      echo \"@@i_manual_cert@@\"\n      echo \"@@i_manual_key@@\"\n      ;;\n")
	b.WriteString("  esac\nfi\n\n")
	b.WriteString("@@c_step7@@\n")
	b.WriteString("case \"$RELOAD_CMD\" in\n  *sudo*)\n")
	b.WriteString("    sudo -k 2>/dev/null || true @@c_sudo_k@@\n")
	b.WriteString("    if ! sudo -n true 2>/dev/null; then\n")
	b.WriteString("      echo \"\" >&2\n")
	b.WriteString("      echo \"@@w_sudo1@@\" >&2\n")
	b.WriteString("      echo \"@@w_sudo2@@\" >&2\n")
	b.WriteString("      echo \"@@w_sudo3@@\" >&2\n")
	b.WriteString("      echo \"@@w_sudo4@@\" >&2\n")
	b.WriteString("      echo \"@@w_sudo5@@\" >&2\n")
	b.WriteString("      echo \"@@w_sudo6@@\" >&2\n")
	b.WriteString("    fi ;;\nesac\n")
	return b.String()
}

var tokenRE = regexp.MustCompile(`@@([a-z0-9_]+)@@`)

// expand는 스크립트 본문의 @@키@@를 언어에 맞는 글로 바꾼다. 표에 없는 키는 그대로 남아 테스트가 잡는다.
func expand(script string, l lang.Lang) string {
	msgs := scriptMessages[string(l)]
	if msgs == nil {
		msgs = scriptMessages[string(lang.Default)]
	}
	return tokenRE.ReplaceAllStringFunc(script, func(tok string) string {
		if v, ok := msgs[tokenRE.FindStringSubmatch(tok)[1]]; ok {
			return v
		}
		return tok
	})
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
func buildSetup(base, dir string, l lang.Lang) setupInfo {
	download := func(file string) (url, cmd string) {
		url = base + "/acme/" + file
		return url, fmt.Sprintf("curl -fsSL %s -o %s && ", url, file)
	}
	su, sc := download(setupFile)
	info := setupInfo{
		Setup: scriptView{ID: "setup", Label: l.Pick("Setup", "세팅"), URL: su, Cmd: sc + "bash " + setupFile, Script: setupScript(base, l)},
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
	return a.Lang.UnknownBaseURL()
}

// setup은 스크립트의 내용과 실행 명령이다. 누구나 볼 수 있다.
func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, buildSetup(a.setupBase(r), "<CERT_DIR>", a.Lang))
}

// scriptFile은 스크립트를 텍스트로 내려준다. 서버에서 curl로 받아 실행한다.
func scriptFile(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

// SetupScript는 GET /acme/setup.sh다. 내려받은 주소(PUBLIC_URL, 없으면 요청 주소)를 주소 기본값으로 넣는다.
func (a *API) SetupScript(w http.ResponseWriter, r *http.Request) {
	scriptFile(w, setupScript(a.PublicURLFor(r), a.Lang))
}
