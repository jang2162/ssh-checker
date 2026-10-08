package adminapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"ssl-checker/internal/llmstxt"
)

// bashCmd는 제어 터미널이 없는 bash 프로세스를 만든다. 스크립트가 /dev/tty로 입력을 기다리지 않게 한다.
func bashCmd(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bashPath(t), "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

func bashPath(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash 없음")
	}
	return bash
}

func allScripts() map[string]string {
	return map[string]string{"setup": setupScript("https://ssl.example.com")}
}

// 생성한 스크립트가 셸 문법에 맞고, 자격 증명 값을 담고 있지 않다.
func TestSetupScriptsAreValidAndHoldNoCredentials(t *testing.T) {
	bash := bashPath(t)
	for name, script := range allScripts() {
		if out, err := exec.Command(bash, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n 실패: %v\n%s", name, err, out)
		}
		for _, bad := range []string{`ACMEDNS_USERNAME="`, `ACMEDNS_PASSWORD="`, `ACMEDNS_SUBDOMAIN="`, "export ACMEDNS_USERNAME=", "export ACMEDNS_PASSWORD="} {
			if strings.Contains(script, bad) {
				t.Errorf("%s: 스크립트에 값이 들어 있다(%s)", name, bad)
			}
		}
		if !strings.Contains(script, `ask ACMEDNS_PASSWORD "password (입력은 보이지 않습니다)" "" secret`) {
			t.Errorf("%s: 비밀번호를 숨겨서 받지 않는다", name)
		}
		if !strings.Contains(script, `ask SSLCHK_URL "ssl-checker 주소" "https://ssl.example.com"`) {
			t.Errorf("%s: 내려받은 주소가 기본값이 아니다", name)
		}
	}
}

// setupEnv는 가짜 acme.sh, curl, sudo를 둔 실행 환경이다. 스크립트가 acme.sh와 /verify를 어떻게 부르는지 기록한다.
type setupEnv struct {
	t       *testing.T
	home    string
	bin     string
	log     string
	certDir string
}

func newSetupEnv(t *testing.T) *setupEnv {
	t.Helper()
	e := &setupEnv{t: t, home: t.TempDir(), bin: t.TempDir()}
	e.log = filepath.Join(e.home, "calls.log")
	e.certDir = filepath.Join(e.home, "certs")
	e.installAcme("v3.1.4")
	// /verify 응답은 VERIFY_CODE, VERIFY_BODY로 정한다. curl 인자(-o 파일)에 본문을 쓰고 상태 코드를 출력한다.
	e.write(filepath.Join(e.bin, "curl"), `#!/bin/sh
case "$*" in
*dns-query*)
  echo "curl doh" >> "$CALLS"
  printf '%s' "${DOH_BODY:-}"
  exit 0 ;;
*get.acme.sh*)
  echo "curl get.acme.sh" >> "$CALLS"
  # get.acme.sh를 받아 sh로 실행하는 것을 흉내 낸다: 가짜 acme.sh를 설치한다.
  printf 'echo "install $*" >> "$CALLS"\nmkdir -p "$HOME/.acme.sh"\nprintf "#!/bin/sh\\n[ \\"\\$1\\" = --version ] && { echo v3.1.4; exit 0; }\\necho \\"acme \\$*\\" >> \\"\\$CALLS\\"\\nexit 0\\n" > "$HOME/.acme.sh/acme.sh"\nchmod +x "$HOME/.acme.sh/acme.sh"\n'
  exit 0 ;;
esac
echo "$@" > "$CALLS.args"
out=""
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
cat > "$CALLS.stdin"
echo "curl" >> "$CALLS"
printf '%s' "${VERIFY_BODY:-{\"ok\":true,\"source_ip\":\"203.0.113.9\",\"subdomain\":\"sub\",\"fulldomain\":\"sub.acme.example.test\"}}" > "$out"
printf '%s' "${VERIFY_CODE:-200}"
`)
	// 가짜 sudo: 기록하고 그대로 실행한다. -n(비밀번호 묻지 않기)일 때 SUDO_NEEDS_PASSWORD가 있으면 실패한다.
	e.write(filepath.Join(e.bin, "sudo"), `#!/bin/sh
echo "sudo $*" >> "$CALLS"
n=""
while [ "$1" = -n ] || [ "$1" = -k ]; do [ "$1" = -n ] && n=1; shift; done
[ -n "$n" ] && [ -n "$SUDO_NEEDS_PASSWORD" ] && exit 1
[ $# -eq 0 ] && exit 0
exec "$@"
`)
	// DNS 조회 도구. DIG_ANSWER가 응답이다(기본은 알맞은 CNAME, 비우면 레코드 없음).
	e.write(filepath.Join(e.bin, "dig"), "#!/bin/sh\necho \"dig $*\" >> \"$CALLS\"\nprintf '%s\\n' \"${DIG_ANSWER-sub.acme.example.test.}\"\n")
	e.write(filepath.Join(e.bin, "host"), "#!/bin/sh\nif [ -n \"$HOST_ANSWER\" ]; then echo \"$2 is an alias for $HOST_ANSWER.\"; else echo \"Host $2 not found: 3(NXDOMAIN)\"; fi\n")
	e.write(filepath.Join(e.bin, "nslookup"), "#!/bin/sh\nif [ -n \"$NS_ANSWER\" ]; then echo \"$3\tcanonical name = $NS_ANSWER.\"; else echo \"** server can't find $3: NXDOMAIN\"; fi\n")
	for _, name := range []string{"nginx", "apache2", "caddy"} { // 설치된 웹서버처럼 보이는 가짜 실행 파일
		e.write(filepath.Join(e.bin, name), "#!/bin/sh\nexit 0\n")
	}
	return e
}

func (e *setupEnv) write(path, body string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// installAcme는 version을 출력하는 가짜 acme.sh를 둔다. 빈 문자열이면 설치하지 않은 상태가 된다.
func (e *setupEnv) installAcme(version string) {
	path := filepath.Join(e.home, ".acme.sh", "acme.sh")
	if version == "" {
		_ = os.Remove(path)
		return
	}
	e.write(path, "#!/bin/sh\n[ \"$1\" = --version ] && { echo https://github.com/acmesh-official/acme.sh; echo "+version+"; exit 0; }\n"+
		"echo \"acme $*\" >> \"$CALLS\"\n[ \"$1\" = --issue ] && echo \"env ACMEDNS_SUBDOMAIN=$ACMEDNS_SUBDOMAIN\" >> \"$CALLS\"\n[ -n \"$ISSUE_EXIT\" ] && [ \"$1\" = --issue ] && exit \"$ISSUE_EXIT\"\nexit 0\n")
}

func (e *setupEnv) run(script string, extra ...string) (string, error) {
	e.t.Helper()
	_ = os.Remove(e.log)
	cmd := bashCmd(e.t, script)
	cmd.Env = append([]string{
		"HOME=" + e.home, "PATH=" + e.bin + ":/usr/bin:/bin", "CALLS=" + e.log,
		"SSLCHK_URL=https://ssl.example.com", "ACMEDNS_USERNAME=u", "ACMEDNS_PASSWORD=p",
		"DOMAIN=a.com", "CERT_DIR=" + e.certDir,
		// 이 컴퓨터에 설치된 웹서버나 설정 디렉터리를 보지 않게 한다(테스트가 시스템에 의존하지 않도록).
		"WEB_SEARCH_DIRS=" + e.bin, "WEB_CONF_DIR=" + filepath.Join(e.home, "no-web-conf"),
		"SUDO_CMD=sudo", // 일반 계정처럼 실행한다. root 동작은 따로 시험한다.
	}, extra...)
	out, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(e.log)
	return string(calls) + string(out), err
}

// 가짜 acme.sh와 sudo로 실제 스크립트를 실행해, 넘긴 값이 acme.sh 인자로 어떻게 가는지 확인한다.
func TestSetupScriptRunsWithEnvValues(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]

	got, err := e.run(setup, "WEB_SERVER=nginx")
	if err != nil {
		t.Fatalf("nginx 실행 실패: %v\n%s", err, got)
	}
	for _, want := range []string{
		"확인됨: acme.sh 3.1.4", "허용 목록에 있습니다",
		`acme --issue --keylength ec-256 --dns dns_acmedns -d a.com -d *.a.com` + "\n", // FORCE가 없으면 --force가 없다
		`acme --install-cert -d a.com --ecc --key-file ` + e.certDir + `/key.pem --fullchain-file ` + e.certDir + `/fullchain.pem --reloadcmd sudo systemctl reload nginx`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("nginx 호출에 %q 없음:\n%s", want, got)
		}
	}
	// acme.sh와 /verify는 <주소>/acme로 부른다(인증서를 받는 서버용 API 접두사).
	if args, _ := os.ReadFile(e.log + ".args"); !strings.Contains(string(args), "https://ssl.example.com/acme/verify") {
		t.Errorf("/verify 주소 = %q", args)
	}
	// subdomain은 입력받지 않고 /verify 응답에서 받아 acme.sh(dns_acmedns)에 넘긴다.
	if !strings.Contains(got, "env ACMEDNS_SUBDOMAIN=sub\n") || strings.Contains(got, "subdomain:") {
		t.Errorf("subdomain은 서버가 알려 준 값을 써야 한다:\n%s", got)
	}
	// 비밀번호는 curl 인자가 아니라 표준입력으로 간다.
	if in, _ := os.ReadFile(e.log + ".stdin"); !strings.Contains(string(in), "X-Api-Key: p") || !strings.Contains(string(in), "X-Api-User: u") {
		t.Errorf("curl 표준입력 = %q", in)
	}

	if got, err = e.run(setup, "WEB_SERVER=nginx", "FORCE=1"); err != nil || !strings.Contains(got, `-d *.a.com --force`) {
		t.Errorf("FORCE=1이면 --force가 붙어야 한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=nginx", "ISSUE_EXIT=2"); err != nil || !strings.Contains(got, "--install-cert") {
		t.Errorf("이미 발급된 경우(종료 코드 2)에도 설치까지 가야 한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=nginx", "ISSUE_EXIT=1"); err == nil {
		t.Errorf("발급 실패는 멈춰야 한다:\n%s", got)
	}
	if got, err = e.run(setup, "WEB_SERVER=apache", "RELOAD_CMD=custom reload"); err != nil || !strings.Contains(got, "--reloadcmd custom reload") {
		t.Errorf("RELOAD_CMD를 넘기면 그대로 쓴다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=apache"); err != nil || !strings.Contains(got, "--reloadcmd sudo systemctl reload apache2 || sudo systemctl reload httpd") {
		t.Errorf("apache 기본 reload: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=iis"); err == nil || !strings.Contains(got, "WEB_SERVER는 nginx, apache, caddy, manual 중 하나여야 합니다") || strings.Contains(got, "--issue") {
		t.Errorf("모르는 웹서버는 발급 전에 멈춰야 한다: err=%v\n%s", err, got)
	}
	// WEB_SERVER를 안 넘기고 터미널도 없으면, 감지된 웹서버(여기서는 셋 다 있어 첫 번째인 nginx)를 기본값으로 쓴다.
	if got, err = e.run(setup); err != nil || !strings.Contains(got, "--reloadcmd sudo systemctl reload nginx") {
		t.Errorf("감지된 첫 웹서버가 기본값: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=caddy"); err != nil || !strings.Contains(got, "sudo chgrp caddy "+e.certDir+"/key.pem && chmod 640 "+e.certDir+"/key.pem && sudo systemctl reload caddy") {
		t.Errorf("caddy는 키 그룹을 맞춘 뒤 reload한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=caddy", "CERT_GROUP=web"); err != nil || !strings.Contains(got, "sudo chgrp web ") {
		t.Errorf("CERT_GROUP을 넘기면 그 그룹을 쓴다: err=%v\n%s", err, got)
	}
}

// acme.sh가 이미 3.1.4 이상이면 설치하지 않고, 없거나 낮으면 허용 IP 확인을 통과한 뒤에 설치한다.
func TestSetupScriptInstallsAcmeWhenMissing(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	const web = "WEB_SERVER=nginx"

	// 이미 설치됨: 설치를 건너뛰고 이메일도 묻지 않는다.
	got, err := e.run(setup, web)
	if err != nil || strings.Contains(got, "get.acme.sh") || strings.Contains(got, "설치합니다") {
		t.Errorf("설치되어 있으면 건너뛰어야 한다: err=%v\n%s", err, got)
	}

	for name, version := range map[string]string{"미설치": "", "낮은 버전": "v3.0.7"} {
		e.installAcme(version)
		// 이메일이 없으면(터미널 없음) 어느 값이 필요한지 알려 주고, 발급 전에 멈춘다.
		got, err := e.run(setup, web)
		if err == nil || !strings.Contains(got, "ACME_EMAIL 값이 필요합니다") || strings.Contains(got, "--issue") {
			t.Errorf("%s: 이메일을 요구하며 멈춰야 한다: err=%v\n%s", name, err, got)
		}
		// 이메일을 넘기면 설치하고 이어서 발급한다.
		e.installAcme(version)
		got, err = e.run(setup, web, "ACME_EMAIL=ops@example.com")
		for _, want := range []string{"curl get.acme.sh", "install email=ops@example.com", "acme.sh 3.1.4 설치 완료", "--issue", "--install-cert"} {
			if err != nil || !strings.Contains(got, want) {
				t.Errorf("%s: 설치 뒤 진행해야 한다(%q 없음): err=%v\n%s", name, want, err, got)
			}
		}
	}

	// 3.1.4와 더 높은 버전은 설치하지 않고 통과한다(sort -V 비교).
	for _, v := range []string{"v3.1.4", "v3.1.10", "v3.2.0", "v10.0.0"} {
		e.installAcme(v)
		if got, err := e.run(setup, web); err != nil || strings.Contains(got, "get.acme.sh") {
			t.Errorf("%s는 설치 없이 통과해야 한다: %v\n%s", v, err, got)
		}
	}
}

// 허용 IP 확인: 자격 증명이 틀리거나 이 서버의 IP가 허용 밖이면 발급 전에 멈추고 이유를 알려 준다.
func TestSetupScriptChecksAllowedIP(t *testing.T) {
	e := newSetupEnv(t)
	nginx := allScripts()["setup"]
	cases := []struct {
		name, code, body, want string
	}{
		{"허용 IP 밖", "403", `{"error":"ip_not_allowed","source_ip":"198.51.100.7"}`, "이 서버의 IP(198.51.100.7)가 허용 IP 목록에 없습니다"},
		{"자격 증명 오류", "401", `{"error":"forbidden"}`, "username 또는 password가 맞지 않습니다"},
		{"차단됨", "429", `{"error":"too_many_attempts"}`, "잠시 후 다시 시도"},
		{"서버 오류", "500", `{"error":"db_error"}`, "HTTP 500"},
	}
	// acme.sh가 없어도 허용 IP 확인이 먼저라서, 거부되면 아무것도 설치하지 않는다.
	e.installAcme("")
	if got, err := e.run(nginx, "WEB_SERVER=nginx", "VERIFY_CODE=403", `VERIFY_BODY={"error":"ip_not_allowed","source_ip":"198.51.100.7"}`, "ACME_EMAIL=ops@example.com"); err == nil || strings.Contains(got, "get.acme.sh") {
		t.Errorf("허용 IP가 아니면 설치하지 않고 멈춰야 한다: err=%v\n%s", err, got)
	}
	e.installAcme("v3.1.4")
	for _, c := range cases {
		got, err := e.run(nginx, "WEB_SERVER=nginx", "VERIFY_CODE="+c.code, "VERIFY_BODY="+c.body)
		if err == nil || !strings.Contains(got, c.want) {
			t.Errorf("%s: %q로 멈춰야 한다: err=%v\n%s", c.name, c.want, err, got)
		}
		if strings.Contains(got, "--issue") || strings.Contains(got, "--install-cert") {
			t.Errorf("%s: 발급까지 가면 안 된다:\n%s", c.name, got)
		}
	}
}

// acme.sh가 있어도 값이 없고 터미널도 아니면, 어느 값이 필요한지 알려 주고 acme.sh를 건드리지 않고 멈춘다.
func TestSetupScriptStopsWithoutValues(t *testing.T) {
	e := newSetupEnv(t)
	for name, script := range allScripts() {
		cmd := bashCmd(t, script)
		cmd.Env = []string{"HOME=" + e.home, "PATH=" + e.bin + ":/usr/bin:/bin", "CALLS=" + e.log, "WEB_SEARCH_DIRS=" + e.bin} // 자격 증명 없음
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "ACMEDNS_USERNAME 값이 필요합니다") {
			t.Errorf("%s: 값이 없으면 멈춰야 한다: err=%v out=%s", name, err, out)
		}
		if calls, _ := os.ReadFile(e.log); strings.Contains(string(calls), "--issue") {
			t.Errorf("%s: 발급까지 갔다: %s", name, calls)
		}
	}
}

func (e *setupEnv) removeWebServers() {
	for _, name := range []string{"nginx", "apache2", "caddy"} {
		_ = os.Remove(filepath.Join(e.bin, name))
	}
}

// 웹서버를 감지해 기본값으로 쓰고, 감지되지 않으면 수동(manual)이 기본이라 reload 스크립트를 직접 받는다.
func TestSetupScriptDetectsWebServer(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]

	got, err := e.run(setup)
	if err != nil || !strings.Contains(got, "감지된 웹서버: nginx apache caddy") || !strings.Contains(got, "확인됨: nginx 실행 파일 ") {
		t.Errorf("설치된 웹서버를 감지해야 한다: err=%v\n%s", err, got)
	}

	e.removeWebServers()
	e.write(filepath.Join(e.bin, "caddy"), "#!/bin/sh\nexit 0\n")
	if got, err = e.run(setup, "CERT_GROUP=web"); err != nil || !strings.Contains(got, "감지된 웹서버: caddy") || !strings.Contains(got, "sudo chgrp web ") {
		t.Errorf("하나만 있으면 그것이 기본값: err=%v\n%s", err, got)
	}

	// 아무것도 감지되지 않으면 수동이 기본이고, reload 스크립트(RELOAD_CMD)가 없으면 발급 전에 멈춘다.
	e.removeWebServers()
	got, err = e.run(setup)
	if err == nil || !strings.Contains(got, "감지된 웹서버가 없습니다. 수동 설정(manual)") || !strings.Contains(got, "RELOAD_CMD 값이 필요합니다") || strings.Contains(got, "--issue") {
		t.Errorf("감지 실패 시 reload 스크립트를 요구해야 한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "RELOAD_CMD=systemctl reload haproxy"); err != nil || !strings.Contains(got, "--reloadcmd systemctl reload haproxy") || !strings.Contains(got, "개인 키:") {
		t.Errorf("수동 설정은 입력한 reload 명령을 쓰고 인증서 경로를 안내한다: err=%v\n%s", err, got)
	}

	// 고른 웹서버가 설치되어 있지 않으면 발급 전에 멈추고 수동 설정을 안내한다. SKIP_WEB_CHECK=1이면 건너뛴다.
	got, err = e.run(setup, "WEB_SERVER=nginx")
	if err == nil || !strings.Contains(got, "nginx 실행 파일을 찾지 못했습니다") || !strings.Contains(got, "WEB_SERVER=manual") || strings.Contains(got, "--issue") {
		t.Errorf("없는 웹서버는 멈춰야 한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=nginx", "SKIP_WEB_CHECK=1"); err != nil || !strings.Contains(got, "--issue") {
		t.Errorf("SKIP_WEB_CHECK=1이면 건너뛴다: err=%v\n%s", err, got)
	}
}

// reload 스크립트 경로를 넘기면 실행할 수 있는지 먼저 확인한다.
func TestSetupScriptChecksReloadScript(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	script := filepath.Join(e.home, "reload-web.sh")

	got, err := e.run(setup, "WEB_SERVER=manual", "RELOAD_CMD="+script)
	if err == nil || !strings.Contains(got, "reload 스크립트를 실행할 수 없습니다: "+script) || strings.Contains(got, "--issue") {
		t.Errorf("없는 스크립트는 발급 전에 멈춰야 한다: err=%v\n%s", err, got)
	}
	e.write(script, "#!/bin/sh\nexit 0\n")
	if got, err = e.run(setup, "WEB_SERVER=manual", "RELOAD_CMD="+script); err != nil || !strings.Contains(got, "--reloadcmd "+script) {
		t.Errorf("실행 가능한 스크립트는 그대로 쓴다: err=%v\n%s", err, got)
	}
	// sudo를 붙여도 스크립트를 확인한다.
	if got, err = e.run(setup, "WEB_SERVER=manual", "RELOAD_CMD=sudo "+filepath.Join(e.home, "nope.sh")); err == nil || !strings.Contains(got, "reload 스크립트를 실행할 수 없습니다") {
		t.Errorf("sudo 뒤의 스크립트도 확인해야 한다: err=%v\n%s", err, got)
	}
}

// 발급 뒤에 웹서버 설정이 이미 이 인증서 경로를 쓰는지 확인하고, 아니면 넣을 설정을 보여 준다.
func TestSetupScriptChecksWebServerConfig(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	conf := filepath.Join(e.home, "nginx-conf")
	e.write(filepath.Join(conf, "sites", "a.conf"), "server { listen 80; }\n")

	got, err := e.run(setup, "WEB_SERVER=nginx", "WEB_CONF_DIR="+conf)
	for _, want := range []string{"확인됨: nginx 설정 디렉터리 " + conf, "nginx 설정에 아래를 넣고 reload하세요", "ssl_certificate     " + e.certDir + "/fullchain.pem;", "ssl_certificate_key " + e.certDir + "/key.pem;"} {
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("설정에 반영되지 않았으면 넣을 설정을 보여 줘야 한다(%q 없음): err=%v\n%s", want, err, got)
		}
	}

	e.write(filepath.Join(conf, "sites", "a.conf"), "server { ssl_certificate "+e.certDir+"/fullchain.pem; }\n")
	if got, err = e.run(setup, "WEB_SERVER=nginx", "WEB_CONF_DIR="+conf); err != nil || !strings.Contains(got, "확인됨: nginx 설정이 이미 이 인증서를 쓰고 있습니다") || strings.Contains(got, "아래를 넣고") {
		t.Errorf("이미 반영되어 있으면 안내만 한다: err=%v\n%s", err, got)
	}

	// 설정 디렉터리를 못 찾아도 발급은 계속하고, 위치를 알려 달라고 안내한다.
	if got, err = e.run(setup, "WEB_SERVER=nginx"); err != nil || !strings.Contains(got, "설정 디렉터리를 찾지 못했습니다") || !strings.Contains(got, "WEB_CONF_DIR") {
		t.Errorf("설정 디렉터리를 못 찾으면 알리고 계속한다: err=%v\n%s", err, got)
	}
}

// 발급 전에 도메인의 _acme-challenge CNAME이 서버가 알려 준 주소를 가리키는지 확인한다.
func TestSetupScriptChecksCNAME(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	const web = "WEB_SERVER=nginx"
	record := "_acme-challenge.a.com.  CNAME  sub.acme.example.test."

	// 맞으면 통과한다. 대소문자와 끝 점 차이는 무시한다.
	got, err := e.run(setup, web, "DIG_ANSWER=SUB.Acme.Example.test.")
	if err != nil || !strings.Contains(got, "확인됨: _acme-challenge.a.com CNAME → sub.acme.example.test") || !strings.Contains(got, "dig +short +time=5 +tries=1 CNAME _acme-challenge.a.com @1.1.1.1") {
		t.Errorf("맞는 CNAME은 통과해야 한다: err=%v\n%s", err, got)
	}

	// 없으면 등록할 레코드를 알려 주고 발급 전에 멈춘다.
	got, err = e.run(setup, web, "DIG_ANSWER=")
	if err == nil || !strings.Contains(got, "CNAME이 없습니다: _acme-challenge.a.com") || !strings.Contains(got, record) || strings.Contains(got, "--issue") {
		t.Errorf("CNAME이 없으면 멈춰야 한다: err=%v\n%s", err, got)
	}
	// 다른 곳을 가리키면 현재 값과 등록할 레코드를 알려 준다.
	got, err = e.run(setup, web, "DIG_ANSWER=other.example.net.")
	if err == nil || !strings.Contains(got, "다른 곳을 가리킵니다: _acme-challenge.a.com → other.example.net") || !strings.Contains(got, record) || strings.Contains(got, "--issue") {
		t.Errorf("다른 CNAME은 멈춰야 한다: err=%v\n%s", err, got)
	}
	if !strings.Contains(got, "SKIP_CNAME_CHECK=1") {
		t.Errorf("건너뛰는 방법을 알려 줘야 한다:\n%s", got)
	}
	// 건너뛰면 경고만 하고 계속한다.
	if got, err = e.run(setup, web, "DIG_ANSWER=", "SKIP_CNAME_CHECK=1"); err != nil || !strings.Contains(got, "확인을 건너뜁니다") || !strings.Contains(got, "--issue") {
		t.Errorf("SKIP_CNAME_CHECK=1이면 계속해야 한다: err=%v\n%s", err, got)
	}
	// 서버가 챌린지 주소를 알려 주지 않으면(옛 서버) 확인을 건너뛴다.
	if got, err = e.run(setup, web, `VERIFY_BODY={"ok":true,"source_ip":"203.0.113.9","subdomain":"sub"}`, "DIG_ANSWER="); err != nil || !strings.Contains(got, "CNAME 확인을 건너뜁니다") {
		t.Errorf("챌린지 주소를 모르면 건너뛴다: err=%v\n%s", err, got)
	}
	// 공개 리졸버는 DNS_RESOLVER로 바꾼다(포트는 떼고 쓴다).
	if got, err = e.run(setup, web, "DNS_RESOLVER=9.9.9.9:53"); err != nil || !strings.Contains(got, "@9.9.9.9") {
		t.Errorf("DNS_RESOLVER를 따라야 한다: err=%v\n%s", err, got)
	}
}

// dig가 없을 때 쓰는 대체 조회(host, nslookup, DNS over HTTPS)도 같은 결과를 낸다.
func TestSetupScriptCNAMELookupFallbacks(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	const web = "WEB_SERVER=nginx"
	ok := "확인됨: _acme-challenge.a.com CNAME → sub.acme.example.test"

	if got, err := e.run(setup, web, "CNAME_TOOL=host", "HOST_ANSWER=sub.acme.example.test"); err != nil || !strings.Contains(got, ok) || strings.Contains(got, "dig +short") {
		t.Errorf("host: err=%v\n%s", err, got)
	}
	if got, err := e.run(setup, web, "CNAME_TOOL=nslookup", "NS_ANSWER=sub.acme.example.test"); err != nil || !strings.Contains(got, ok) {
		t.Errorf("nslookup: err=%v\n%s", err, got)
	}
	doh := `{"Status":0,"Answer":[{"name":"_acme-challenge.a.com","type":5,"TTL":300,"data":"sub.acme.example.test."}]}`
	if got, err := e.run(setup, web, "CNAME_TOOL=doh", "DOH_BODY="+doh); err != nil || !strings.Contains(got, ok) || !strings.Contains(got, "curl doh") {
		t.Errorf("doh: err=%v\n%s", err, got)
	}
	// 대체 조회에서도 없으면 멈춘다.
	if got, err := e.run(setup, web, "CNAME_TOOL=host"); err == nil || !strings.Contains(got, "CNAME이 없습니다") {
		t.Errorf("host로 못 찾으면 멈춰야 한다: err=%v\n%s", err, got)
	}
	if got, err := e.run(setup, web, "CNAME_TOOL=doh", `DOH_BODY={"Status":3}`); err == nil || !strings.Contains(got, "CNAME이 없습니다") {
		t.Errorf("DoH로 못 찾으면 멈춰야 한다: err=%v\n%s", err, got)
	}
}

// 서버가 알려 준 subdomain을 쓰므로 입력받지 않는다. 옛 서버처럼 알려 주지 않으면 환경변수로 받는다.
func TestSetupScriptTakesSubdomainFromServer(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	got, err := e.run(setup, "WEB_SERVER=nginx")
	if err != nil || strings.Contains(got, "subdomain:") || !strings.Contains(got, "env ACMEDNS_SUBDOMAIN=sub\n") {
		t.Errorf("서버가 알려 준 subdomain을 써야 한다: err=%v\n%s", err, got)
	}
	// 환경변수로 다른 값을 넘겨도 계정에 묶인 서버 값이 우선한다(다르면 /update가 401로 거부하므로).
	if got, err = e.run(setup, "WEB_SERVER=nginx", "ACMEDNS_SUBDOMAIN=other"); err != nil || !strings.Contains(got, "env ACMEDNS_SUBDOMAIN=sub\n") {
		t.Errorf("서버 값이 우선해야 한다: err=%v\n%s", err, got)
	}
	// 서버가 알려 주지 않고 환경변수도 없으면 멈춘다.
	body := `VERIFY_BODY={"ok":true,"source_ip":"203.0.113.9","fulldomain":"sub.acme.example.test"}`
	if got, err = e.run(setup, "WEB_SERVER=nginx", body); err == nil || !strings.Contains(got, "서버가 subdomain을 알려 주지 않았습니다") || strings.Contains(got, "--issue") {
		t.Errorf("subdomain을 알 수 없으면 멈춰야 한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=nginx", body, "ACMEDNS_SUBDOMAIN=sub"); err != nil || !strings.Contains(got, "env ACMEDNS_SUBDOMAIN=sub\n") {
		t.Errorf("옛 서버는 환경변수로 받는다: err=%v\n%s", err, got)
	}
}

// 도메인은 항상 와일드카드와 함께 발급한다. *.를 붙여 입력해도 같은 도메인으로 본다.
func TestSetupScriptIssuesWildcardWithDomain(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	got, err := e.run(setup, "WEB_SERVER=nginx")
	if err != nil || !strings.Contains(got, "-d a.com -d *.a.com") || strings.Contains(got, "알림: *.는 빼고") {
		t.Errorf("도메인과 와일드카드를 함께 발급해야 한다: err=%v\n%s", err, got)
	}
	got, err = e.run(setup, "WEB_SERVER=nginx", "DOMAIN=*.a.com")
	if err != nil || !strings.Contains(got, "알림: *.는 빼고 a.com으로 진행합니다") || !strings.Contains(got, "-d a.com -d *.a.com") ||
		strings.Contains(got, "**.a.com") || !strings.Contains(got, "_acme-challenge.a.com CNAME") {
		t.Errorf("*.a.com을 입력하면 a.com으로 보고 진행해야 한다: err=%v\n%s", err, got)
	}
}

// 저장 디렉터리를 sudo 없이 쓸 수 있으면 sudo를 쓰지 않는다. 쓸 수 없을 때만 sudo로 만든다.
func TestSetupScriptAvoidsSudoForWritableCertDir(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	got, err := e.run(setup, "WEB_SERVER=nginx")
	if err != nil || strings.Contains(got, "sudo install") || strings.Contains(got, "sudo가 필요합니다") {
		t.Errorf("쓸 수 있는 디렉터리는 sudo 없이 만들어야 한다: err=%v\n%s", err, got)
	}
	if _, statErr := os.Stat(e.certDir); statErr != nil {
		t.Errorf("저장 디렉터리가 만들어져야 한다: %v", statErr)
	}

	if os.Geteuid() == 0 {
		t.Skip("root는 어디든 쓸 수 있어 sudo 경로를 시험할 수 없다")
	}
	locked := filepath.Join(e.home, "locked")
	if err := os.MkdirAll(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)
	got, _ = e.run(setup, "WEB_SERVER=nginx", "CERT_DIR="+filepath.Join(locked, "certs"))
	if !strings.Contains(got, "저장 디렉터리("+filepath.Join(locked, "certs")+")를 만들려면 sudo가 필요합니다") || !strings.Contains(got, "sudo install -d -o ") {
		t.Errorf("쓸 수 없으면 sudo로 만들고 이유를 알려야 한다:\n%s", got)
	}
}

// 자동 갱신(cron)은 sudo 비밀번호를 입력할 수 없다. reload가 sudo를 쓰는데 비밀번호가 필요하면 미리 경고한다.
func TestSetupScriptWarnsWhenReloadNeedsSudoPassword(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]

	got, err := e.run(setup, "WEB_SERVER=nginx", "SUDO_NEEDS_PASSWORD=1")
	for _, want := range []string{"경고: reload 명령이 sudo를 쓰는데, 이 계정은 sudo에 비밀번호가 필요합니다", "자동 갱신(cron)은 비밀번호를 입력할 수 없어서", "NOPASSWD", "sudo 없이 실행되는 reload 스크립트"} {
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("경고에 %q가 있어야 한다: err=%v\n%s", want, err, got)
		}
	}
	// 방금 입력한 sudo 비밀번호의 캐시가 점검을 가리지 않도록 캐시를 지운다.
	if !strings.Contains(got, "sudo -k") {
		t.Errorf("sudo 캐시를 지워야 정확히 점검된다:\n%s", got)
	}
	// 비밀번호 없이 sudo가 되면 경고하지 않는다.
	if got, err = e.run(setup, "WEB_SERVER=nginx"); err != nil || strings.Contains(got, "경고: reload 명령이 sudo") {
		t.Errorf("비밀번호 없이 되면 경고하지 않는다: err=%v\n%s", err, got)
	}
	// reload 명령이 sudo를 쓰지 않으면 점검하지 않는다.
	if got, err = e.run(setup, "WEB_SERVER=manual", "RELOAD_CMD=docker exec caddy caddy reload", "SUDO_NEEDS_PASSWORD=1"); err != nil || strings.Contains(got, "경고: reload 명령이 sudo") {
		t.Errorf("sudo를 쓰지 않으면 경고하지 않는다: err=%v\n%s", err, got)
	}
}

// root이면 sudo가 필요 없어서 reload 기본 명령과 디렉터리 생성에서 sudo를 뺀다. 일반 계정이면 sudo를 쓴다.
func TestSetupScriptDropsSudoForRoot(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	for web, want := range map[string]string{
		"nginx":  "--reloadcmd systemctl reload nginx",
		"apache": "--reloadcmd systemctl reload apache2 || systemctl reload httpd",
		"caddy":  "chgrp caddy " + e.certDir + "/key.pem && chmod 640 " + e.certDir + "/key.pem && systemctl reload caddy",
	} {
		got, err := e.run(setup, "WEB_SERVER="+web, "SUDO_CMD=", "SUDO_NEEDS_PASSWORD=1")
		if err != nil || !strings.Contains(got, want) || strings.Contains(got, "sudo") || strings.Contains(got, "경고: reload 명령이 sudo") {
			t.Errorf("%s: root에서는 sudo가 없어야 한다(%q): err=%v\n%s", web, want, err, got)
		}
	}
	// 일반 계정은 sudo를 붙인다.
	if got, err := e.run(setup, "WEB_SERVER=nginx"); err != nil || !strings.Contains(got, "--reloadcmd sudo systemctl reload nginx") {
		t.Errorf("일반 계정은 sudo를 붙여야 한다: err=%v\n%s", err, got)
	}
}

// SUDO_CMD를 정하지 않으면 실행하는 계정(id -u)을 따른다: root는 sudo 없음, 그 밖에는 sudo.
func TestSetupScriptSudoDefaultFollowsAccount(t *testing.T) {
	e := newSetupEnv(t)
	setup := allScripts()["setup"]
	cmd := bashCmd(t, setup)
	cmd.Env = []string{
		"HOME=" + e.home, "PATH=" + e.bin + ":/usr/bin:/bin", "CALLS=" + e.log, "WEB_SEARCH_DIRS=" + e.bin, "WEB_CONF_DIR=" + filepath.Join(e.home, "x"),
		"SSLCHK_URL=https://ssl.example.com", "ACMEDNS_USERNAME=u", "ACMEDNS_PASSWORD=p", "DOMAIN=a.com", "CERT_DIR=" + e.certDir, "WEB_SERVER=nginx",
	}
	out, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(e.log)
	got := string(calls) + string(out)
	want := "--reloadcmd sudo systemctl reload nginx"
	if os.Geteuid() == 0 {
		want = "--reloadcmd systemctl reload nginx"
	}
	if err != nil || !strings.Contains(got, want) {
		t.Errorf("계정(euid=%d)에 맞는 reload 기본값이어야 한다(%q): err=%v\n%s", os.Geteuid(), want, err, got)
	}
}

// 스크립트가 읽는 환경변수는 문서와 llms.txt에 모두 있어야 한다. 변수를 추가하고 문서를 빼먹는 일을 막는다.
func TestSetupVariablesAreDocumented(t *testing.T) {
	script := setupScript("https://ssl.example.com")
	vars := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*ask ([A-Z][A-Z_]+)`),      // 물어보는 값
		regexp.MustCompile(`\$\{([A-Z][A-Z_]+)(?::-|:\+|\+)`), // 환경에서 읽는 선택 값
	} {
		for _, m := range re.FindAllStringSubmatch(script, -1) {
			vars[m[1]] = true
		}
	}
	// 스크립트 안에서만 쓰는 값과 시험용 훅은 문서에 쓰지 않는다.
	for _, internal := range []string{"CNAME_TOOL", "WEB_DEFAULT", "RELOAD_DEFAULT", "DETECTED"} {
		delete(vars, internal)
	}
	if len(vars) < 15 {
		t.Fatalf("변수를 제대로 추출하지 못했다: %v", vars)
	}

	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	llms, err := llmstxt.Render(llmstxt.Data{BaseURL: "https://ssl.example.com", Zone: "acme.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{
		"docs/guide/api.md":        read("../../docs/guide/api.md"),
		"docs/guide/onboarding.md": read("../../docs/guide/onboarding.md"),
		"llms.txt":                 llms,
	}
	// llms.txt는 에이전트가 따라 하는 문서라, 자주 안 쓰는 탐색 위치(WEB_SEARCH_DIRS)는 빼고 모두 있어야 한다.
	llmsOptional := map[string]bool{"WEB_SEARCH_DIRS": true}
	for v := range vars {
		for name, text := range docs {
			if name == "llms.txt" && llmsOptional[v] {
				continue
			}
			if !strings.Contains(text, v) {
				t.Errorf("setup.sh가 읽는 %s가 %s에 없다", v, name)
			}
		}
	}
}
