package adminapi

import (
	"fmt"
	"net/http"
	"strings"

	"ssl-checker/internal/httpx"
)

// webServer는 인증서를 받을 서버에서 쓰는 웹서버별 차이(reload 명령, 키 파일 권한, 설정 예)다.
type webServer struct {
	ID    string
	Label string
	// ReloadDefault는 RELOAD_CMD를 비웠을 때 쓰는 명령이다. 셸이 변수를 풀어서 보여 준다.
	ReloadDefault string
	// Group이면 키 파일을 읽을 그룹을 묻는다(caddy는 일반 사용자로 돈다).
	Group bool
	// Config는 웹서버 설정 예다. %[1]s는 인증서 디렉터리로 바뀐다.
	Config string
}

var webServers = []webServer{
	{
		ID: "nginx", Label: "nginx",
		ReloadDefault: "sudo systemctl reload nginx",
		Config:        "ssl_certificate     %[1]s/fullchain.pem;\nssl_certificate_key %[1]s/key.pem;\n",
	},
	{
		ID: "apache", Label: "Apache",
		// Debian 계열은 apache2, RHEL 계열은 httpd다.
		ReloadDefault: "sudo systemctl reload apache2 || sudo systemctl reload httpd",
		Config:        "SSLCertificateFile    %[1]s/fullchain.pem\nSSLCertificateKeyFile %[1]s/key.pem\n",
	},
	{
		ID: "caddy", Label: "Caddy",
		// caddy는 키 파일을 읽을 수 있어야 해서, 그룹을 맞춘 뒤 reload한다.
		ReloadDefault: "sudo chgrp $CERT_GROUP $CERT_DIR/key.pem && chmod 640 $CERT_DIR/key.pem && sudo systemctl reload caddy",
		Group:         true,
		Config:        "example.com, *.example.com {\n\ttls %[1]s/fullchain.pem %[1]s/key.pem\n}\n",
	},
}

// askFunc는 값을 물어보는 셸 함수다. 환경변수로 미리 받은 값은 묻지 않아서 에이전트와 자동화도 쓸 수 있다.
// 비밀번호는 화면에 보이지 않게 받고, 파일이나 셸 기록에 남지 않는다.
const askFunc = `die() { echo "$*" >&2; exit 1; }

# ask 변수이름 "설명" [기본값] [secret]
ask() {
  local var=$1 label=$2 def=${3:-} secret=${4:-} val
  if [ -n "${!var:-}" ]; then return; fi
  if [ -t 0 ]; then
    if [ -n "$secret" ]; then
      read -rsp "$label: " val; echo
    else
      read -rp "$label${def:+ [$def]}: " val
    fi
  fi
  # 터미널이 없으면(에이전트, 자동화) 기본값을 쓴다. 기본값이 없는 값은 환경변수로 넘겨야 한다.
  val=${val:-$def}
  [ -n "$val" ] || die "$var 값이 필요합니다. 터미널에서 실행하거나 환경변수로 넘기세요."
  printf -v "$var" '%s' "$val"
  export "$var"
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
  local tmp code ip
  tmp=$(mktemp)
  code=$(printf 'header = "X-Api-User: %s"\nheader = "X-Api-Key: %s"\n' "$ACMEDNS_USERNAME" "$ACMEDNS_PASSWORD" |
    curl -sS -K - -o "$tmp" -w '%{http_code}' -X POST "$ACMEDNS_BASE_URL/verify") ||
    { rm -f "$tmp"; die "ssl-checker($ACMEDNS_BASE_URL)에 연결하지 못했습니다. 주소와 네트워크를 확인하세요."; }
  ip=$(sed -n 's/.*"source_ip":"\([^"]*\)".*/\1/p' "$tmp")
  rm -f "$tmp"
  case "$code" in
    200) echo "확인됨: 자격 증명이 맞고, 이 서버의 IP(${ip:-알 수 없음})가 허용 목록에 있습니다." ;;
    401) die "username 또는 password가 맞지 않습니다." ;;
    403) die "이 서버의 IP(${ip:-알 수 없음})가 허용 IP 목록에 없습니다. 관리자에게 이 IP를 허용 목록에 추가해 달라고 요청하세요." ;;
    429) die "인증 실패가 너무 많아 잠시 차단됐습니다. 잠시 후 다시 시도하세요." ;;
    *) die "자격 증명 확인에 실패했습니다(HTTP $code)." ;;
  esac
}
`

// installScript는 acme.sh를 설치한다. 서버마다 한 번만 실행하고, 갱신용 cron은 설치가 등록한다.
func installScript() string {
	return "#!/usr/bin/env bash\n" +
		"# acme.sh 설치. 이미 설치되어 있으면 아무것도 하지 않는다.\n" +
		"# 실행하면 값을 물어본다. 환경변수 ACME_EMAIL로 미리 넘기면 묻지 않는다.\n" +
		"set -eu\n\n" + askFunc + "\n" + acmeFuncs + "\n" +
		"command -v curl >/dev/null || die \"curl이 필요합니다. 먼저 설치하세요.\"\n" +
		"if acme_ok; then\n" +
		"  echo \"acme.sh $(acme_version)가 이미 설치되어 있습니다. 설치를 건너뜁니다.\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ -x \"$ACME_SH\" ]; then\n" +
		"  echo \"acme.sh가 3.1.4보다 낮습니다. 업그레이드합니다.\"\n" +
		"fi\n\n" +
		"ask ACME_EMAIL \"인증서 만료 알림을 받을 메일 주소\"\n\n" +
		"# cron이 있어야 자동 갱신이 등록된다.\n" +
		"curl -fsSL https://get.acme.sh | sh -s email=\"$ACME_EMAIL\"\n" +
		"acme_ok || die \"설치 뒤에도 acme.sh 3.1.4 이상을 확인하지 못했습니다.\"\n" +
		"echo \"acme.sh $(acme_version) 설치 완료\"\n"
}

// setupScript는 인증서를 받을 서버에서 내려받아 실행하는 세팅 스크립트다(acme.sh 3.1.4 이상).
// 웹서버(nginx, apache, caddy)는 실행할 때 고른다. 자격 증명은 스크립트에 넣지 않고 실행할 때 입력받는다.
// baseURL은 내려받은 주소라서 기본값으로만 쓴다.
func setupScript(baseURL string) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n# 인증서 발급·설치. 실행하면 값을 물어본다.\n")
	b.WriteString("# 환경변수로 미리 넘기면 묻지 않는다. 키를 재발급한 뒤에는 FORCE=1로 실행한다.\n")
	b.WriteString("set -eu\n\n" + askFunc + "\n" + acmeFuncs + "\n" + verifyFunc + "\n")
	b.WriteString("command -v curl >/dev/null || die \"curl이 필요합니다. 먼저 설치하세요.\"\n\n")
	b.WriteString("ask SSLCHK_URL \"ssl-checker 주소\"")
	if baseURL != "" {
		fmt.Fprintf(&b, " %q", baseURL)
	}
	b.WriteString("\nSSLCHK_URL=${SSLCHK_URL%/}\n")
	b.WriteString("# acme.sh의 dns_acmedns가 부르는 주소. 인증서를 받는 서버용 API와 스크립트는 /acme/ 아래에 있다.\n")
	b.WriteString("ACMEDNS_BASE_URL=\"${ACMEDNS_BASE_URL:-$SSLCHK_URL/acme}\"; export ACMEDNS_BASE_URL\n")
	b.WriteString("\n# 1) acme.sh가 설치되어 있는지 확인한다.\n")
	b.WriteString("acme_ok || die \"acme.sh 3.1.4 이상이 설치되어 있지 않습니다. 먼저 설치하세요:\n  curl -fsSL $SSLCHK_URL/acme/install.sh -o install.sh && bash install.sh\"\n")
	b.WriteString("echo \"확인됨: acme.sh $(acme_version)\"\n\n")
	b.WriteString("# 2) 자격 증명을 입력받고, 이 서버의 IP가 허용 목록에 있는지 확인한다.\n")
	b.WriteString("ask ACMEDNS_USERNAME \"username\"\n")
	b.WriteString("ask ACMEDNS_PASSWORD \"password (입력은 보이지 않습니다)\" \"\" secret\n")
	b.WriteString("ask ACMEDNS_SUBDOMAIN \"subdomain\"\n")
	b.WriteString("verify_server\n\n")
	b.WriteString("# 3) 인증서를 발급해 설치한다.\n")
	ids := make([]string, 0, len(webServers))
	for _, ws := range webServers {
		ids = append(ids, ws.ID)
	}
	fmt.Fprintf(&b, "ask WEB_SERVER \"웹서버 (%s)\" \"%s\"\n", strings.Join(ids, ", "), webServers[0].ID)
	b.WriteString("ask DOMAIN \"인증서를 받을 도메인 (예: example.com)\"\n")
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
	fmt.Fprintf(&b, "  *) die \"WEB_SERVER는 %s 중 하나여야 합니다.\" ;;\nesac\n", strings.Join(ids, ", "))
	b.WriteString("ask RELOAD_CMD \"설치 뒤 실행할 명령\" \"$RELOAD_DEFAULT\"\n\n")
	b.WriteString("\"$ACME_SH\" --set-default-ca --server letsencrypt\n")
	b.WriteString("# 처음 연결하는 도메인은 --issue에 --staging을 붙여 먼저 확인한다(docs 참고).\n")
	b.WriteString("# FORCE=1이면 이미 인증서가 있어도 다시 발급해, 새 키를 도메인 설정에 저장한다.\n")
	// 종료 코드 2는 "아직 갱신할 때가 아님"이다. 스크립트를 다시 돌려도 멈추지 않게 한다.
	b.WriteString(`"$ACME_SH" --issue --keylength ec-256 --dns dns_acmedns -d "$DOMAIN" -d "*.$DOMAIN" ${FORCE:+--force} || [ "$?" -eq 2 ]` + "\n")
	b.WriteString("sudo install -d -o \"$(id -un)\" \"$CERT_DIR\"\n")
	b.WriteString("\"$ACME_SH\" --install-cert -d \"$DOMAIN\" --ecc \\\n")
	b.WriteString("  --key-file \"$CERT_DIR/key.pem\" \\\n")
	b.WriteString("  --fullchain-file \"$CERT_DIR/fullchain.pem\" \\\n")
	b.WriteString("  --reloadcmd \"$RELOAD_CMD\"\n")
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

// serverView는 웹서버 하나의 세팅 명령과 웹서버 설정 예다. 스크립트는 같고 WEB_SERVER만 다르다.
type serverView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Cmd    string `json:"cmd"`
	Config string `json:"config"`
}

// setupInfo는 acme.sh 설치와 세팅 스크립트, 웹서버별 실행 명령이다.
type setupInfo struct {
	Install scriptView   `json:"install"`
	Setup   scriptView   `json:"setup"`
	Servers []serverView `json:"servers"`
}

// 스크립트는 인증서를 받는 서버용 API와 같은 접두사(/acme/) 아래에 둔다. 프록시에서 /acme/* 한 줄로 열 수 있다.
const (
	installFile = "install.sh"
	setupFile   = "setup.sh"
)

// buildSetup은 base 주소 기준의 내려받기 주소와 실행 명령을 만든다. dir은 설정 예에 쓸 인증서 디렉터리다.
func buildSetup(base, dir string) setupInfo {
	download := func(file string) (url, cmd string) {
		url = base + "/acme/" + file
		return url, fmt.Sprintf("curl -fsSL %s -o %s && ", url, file)
	}
	iu, ic := download(installFile)
	su, sc := download(setupFile)
	info := setupInfo{
		Install: scriptView{ID: "install", Label: "acme.sh 설치", URL: iu, Cmd: ic + "bash " + installFile, Script: installScript()},
		Setup:   scriptView{ID: "setup", Label: "세팅", URL: su, Cmd: sc + "bash " + setupFile, Script: setupScript(base)},
	}
	for _, ws := range webServers {
		info.Servers = append(info.Servers, serverView{ID: ws.ID, Label: ws.Label, Cmd: sc + "WEB_SERVER=" + ws.ID + " bash " + setupFile, Config: fmt.Sprintf(ws.Config, dir)})
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

// setup은 설치·세팅 스크립트의 내용과 실행 명령이다. 누구나 볼 수 있다.
func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, buildSetup(a.setupBase(r), "<CERT_DIR>"))
}

// scriptFile은 스크립트를 텍스트로 내려준다. 서버에서 curl로 받아 실행한다.
func scriptFile(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

// InstallScript는 GET /acme/install.sh다.
func (a *API) InstallScript(w http.ResponseWriter, r *http.Request) { scriptFile(w, installScript()) }

// SetupScript는 GET /acme/setup.sh다. 내려받은 주소(PUBLIC_URL, 없으면 요청 주소)를 주소 기본값으로 넣는다.
func (a *API) SetupScript(w http.ResponseWriter, r *http.Request) {
	scriptFile(w, setupScript(a.PublicURLFor(r)))
}
