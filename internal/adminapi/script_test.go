package adminapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func bashPath(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash 없음")
	}
	return bash
}

func allScripts() map[string]string {
	return map[string]string{"install": installScript(), "setup": setupScript("https://ssl.example.com")}
}

// 생성한 스크립트가 셸 문법에 맞고, 자격 증명 값을 담고 있지 않다.
func TestSetupScriptsAreValidAndHoldNoCredentials(t *testing.T) {
	bash := bashPath(t)
	for name, script := range allScripts() {
		if out, err := exec.Command(bash, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: bash -n 실패: %v\n%s", name, err, out)
		}
		if name == "install" {
			continue
		}
		for _, bad := range []string{`ACMEDNS_USERNAME="`, `ACMEDNS_PASSWORD="`, `ACMEDNS_SUBDOMAIN="`, "export ACMEDNS_USERNAME", "export ACMEDNS_PASSWORD", "export ACMEDNS_SUBDOMAIN"} {
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
echo "$@" > "$CALLS.args"
out=""
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
cat > "$CALLS.stdin"
echo "curl" >> "$CALLS"
printf '%s' "${VERIFY_BODY:-{\"ok\":true,\"source_ip\":\"203.0.113.9\"}}" > "$out"
printf '%s' "${VERIFY_CODE:-200}"
`)
	e.write(filepath.Join(e.bin, "sudo"), "#!/bin/sh\necho \"sudo $*\" >> \"$CALLS\"\nexit 0\n")
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
		"echo \"acme $*\" >> \"$CALLS\"\n[ -n \"$ISSUE_EXIT\" ] && [ \"$1\" = --issue ] && exit \"$ISSUE_EXIT\"\nexit 0\n")
}

func (e *setupEnv) run(script string, extra ...string) (string, error) {
	e.t.Helper()
	_ = os.Remove(e.log)
	cmd := exec.Command(bashPath(e.t), "-c", script)
	cmd.Env = append([]string{
		"HOME=" + e.home, "PATH=" + e.bin + ":/usr/bin:/bin", "CALLS=" + e.log,
		"SSLCHK_URL=https://ssl.example.com", "ACMEDNS_USERNAME=u", "ACMEDNS_PASSWORD=p", "ACMEDNS_SUBDOMAIN=s",
		"DOMAIN=a.com", "CERT_DIR=" + e.certDir,
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
	if got, err = e.run(setup, "WEB_SERVER=iis"); err == nil || !strings.Contains(got, "WEB_SERVER는 nginx, apache, caddy 중 하나여야 합니다") || strings.Contains(got, "--issue") {
		t.Errorf("모르는 웹서버는 발급 전에 멈춰야 한다: err=%v\n%s", err, got)
	}
	// WEB_SERVER를 안 넘기고 터미널도 없으면 기본값(nginx)을 쓴다.
	if got, err = e.run(setup); err != nil || !strings.Contains(got, "--reloadcmd sudo systemctl reload nginx") {
		t.Errorf("웹서버 기본값은 nginx: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=caddy"); err != nil || !strings.Contains(got, "sudo chgrp caddy "+e.certDir+"/key.pem && chmod 640 "+e.certDir+"/key.pem && sudo systemctl reload caddy") {
		t.Errorf("caddy는 키 그룹을 맞춘 뒤 reload한다: err=%v\n%s", err, got)
	}
	if got, err = e.run(setup, "WEB_SERVER=caddy", "CERT_GROUP=web"); err != nil || !strings.Contains(got, "sudo chgrp web ") {
		t.Errorf("CERT_GROUP을 넘기면 그 그룹을 쓴다: err=%v\n%s", err, got)
	}
}

// acme.sh가 없거나 3.1.4보다 낮으면 자격 증명을 묻거나 서버를 부르기 전에 멈추고, 설치 방법을 알려 준다.
func TestSetupScriptChecksAcmeInstall(t *testing.T) {
	e := newSetupEnv(t)
	nginx := allScripts()["setup"]
	for name, version := range map[string]string{"미설치": "", "낮은 버전": "v3.0.7"} {
		e.installAcme(version)
		got, err := e.run(nginx, "WEB_SERVER=nginx")
		if err == nil || !strings.Contains(got, "acme.sh 3.1.4 이상이 설치되어 있지 않습니다") ||
			!strings.Contains(got, "curl -fsSL https://ssl.example.com/acme/install.sh -o install.sh && bash install.sh") {
			t.Errorf("%s: 설치 안내 없이 멈춰야 한다: err=%v\n%s", name, err, got)
		}
		if strings.Contains(got, "curl\n") || strings.Contains(got, "--issue") {
			t.Errorf("%s: 서버를 부르거나 발급하면 안 된다:\n%s", name, got)
		}
	}
	// 3.1.4와 더 높은 버전은 통과한다(sort -V 비교).
	for _, v := range []string{"v3.1.4", "v3.1.10", "v3.2.0", "v10.0.0"} {
		e.installAcme(v)
		if got, err := e.run(nginx, "WEB_SERVER=nginx"); err != nil {
			t.Errorf("%s는 통과해야 한다: %v\n%s", v, err, got)
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

// 설치 스크립트는 이미 설치되어 있으면 아무것도 하지 않고, 없으면 설치한다.
func TestInstallScript(t *testing.T) {
	e := newSetupEnv(t)
	install := allScripts()["install"]
	if got, err := e.run(install); err != nil || !strings.Contains(got, "이미 설치되어 있습니다") || strings.Contains(got, "curl\n") {
		t.Errorf("설치된 경우 건너뛰어야 한다: err=%v\n%s", err, got)
	}
	e.installAcme("")
	got, err := e.run(install)
	if err == nil || !strings.Contains(got, "ACME_EMAIL 값이 필요합니다") {
		t.Errorf("미설치이면 이메일을 요구해야 한다: err=%v\n%s", err, got)
	}
}

// acme.sh가 있어도 값이 없고 터미널도 아니면, 어느 값이 필요한지 알려 주고 acme.sh를 건드리지 않고 멈춘다.
func TestSetupScriptStopsWithoutValues(t *testing.T) {
	bash := bashPath(t)
	e := newSetupEnv(t)
	for name, script := range allScripts() {
		if name == "install" {
			continue
		}
		cmd := exec.Command(bash, "-c", script)
		cmd.Env = []string{"HOME=" + e.home, "PATH=" + e.bin + ":/usr/bin:/bin", "CALLS=" + e.log} // 자격 증명 없음
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "ACMEDNS_USERNAME 값이 필요합니다") {
			t.Errorf("%s: 값이 없으면 멈춰야 한다: err=%v out=%s", name, err, out)
		}
		if calls, _ := os.ReadFile(e.log); strings.Contains(string(calls), "--issue") {
			t.Errorf("%s: 발급까지 갔다: %s", name, calls)
		}
	}
}
