package adminapi

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"ssl-checker/internal/lang"
)

var hangul = regexp.MustCompile("[가-힣]")

// 스크립트가 쓰는 모든 키가 두 언어 표에 있고, 쓰지 않는 키는 없다. 영어 글에는 한글이 없다.
func TestScriptMessagesAreComplete(t *testing.T) {
	raw := setupScriptTokens("https://ssl.example.com")
	used := map[string]bool{}
	for _, m := range tokenRE.FindAllStringSubmatch(raw, -1) {
		used[m[1]] = true
	}
	if len(used) < 60 {
		t.Fatalf("키를 제대로 찾지 못했다: %d개", len(used))
	}
	for _, l := range []string{"ko", "en"} {
		table := scriptMessages[l]
		for key := range used {
			if table[key] == "" {
				t.Errorf("%s 표에 %q가 없다", l, key)
			}
		}
		var unused []string
		for key := range table {
			if !used[key] {
				unused = append(unused, key)
			}
		}
		sort.Strings(unused)
		if len(unused) > 0 {
			t.Errorf("%s 표에 쓰지 않는 키가 있다: %v", l, unused)
		}
	}
	for key, v := range scriptMessages["en"] {
		if hangul.MatchString(v) {
			t.Errorf("영어 글에 한글이 있다 (%s): %q", key, v)
		}
		if !strings.HasPrefix(key, "c_") && strings.ContainsAny(v, "\"`") { // c_*는 셸 주석 줄이라 따옴표가 있어도 된다
			t.Errorf("영어 글에 셸 인용을 깨는 문자가 있다 (%s): %q", key, v)
		}
	}
	for key, v := range scriptMessages["ko"] {
		if !strings.HasPrefix(key, "c_") && strings.ContainsAny(v, "\"`") {
			t.Errorf("한국어 글에 셸 인용을 깨는 문자가 있다 (%s): %q", key, v)
		}
		// 두 언어는 같은 셸 변수를 보여 줘야 한다($var, ${ip:-...}, $(acme_version) 등).
		if shellRefs(v) != shellRefs(scriptMessages["en"][key]) {
			t.Errorf("%s: 셸 변수 참조가 언어마다 다르다: ko %q / en %q", key, shellRefs(v), shellRefs(scriptMessages["en"][key]))
		}
	}
	for _, l := range []lang.Lang{lang.KO, lang.EN} {
		out := setupScript("https://ssl.example.com", l)
		if strings.Contains(out, "@@") {
			t.Errorf("%s: 풀리지 않은 키가 남았다", l)
		}
	}
	if hangul.MatchString(setupScript("https://ssl.example.com", lang.EN)) {
		t.Error("영어 스크립트에 한글이 남았다")
	}
}

var shellRefRE = regexp.MustCompile(`\$\{[^}]*\}|\$\([a-z_]+\)|\$[A-Za-z_][A-Za-z0-9_]*`)

// shellRefs는 글 속 셸 변수·명령 참조를 정렬해 이어 붙인다. 번역해도 같아야 한다(${ip:-알 수 없음}은 기본 문구만 다르다).
func shellRefs(s string) string {
	refs := shellRefRE.FindAllString(s, -1)
	for i, r := range refs {
		if strings.HasPrefix(r, "${ip:-") {
			refs[i] = "${ip}"
		}
	}
	sort.Strings(refs)
	return strings.Join(refs, " ")
}

// 영어 스크립트도 같은 순서로 같은 일을 하고, 메시지가 영어로 나온다.
func TestSetupScriptRunsInEnglish(t *testing.T) {
	e := newSetupEnv(t)
	en := setupScript("https://ssl.example.com", lang.EN)

	got, err := e.run(en, "WEB_SERVER=nginx")
	for _, want := range []string{
		"Verified: the credentials are valid and this server's IP (203.0.113.9) is on the allowed list.",
		"Verified: acme.sh 3.1.4", "Detected web servers: nginx apache caddy", "Verified: nginx executable ",
		"Verified: _acme-challenge.a.com CNAME -> sub.acme.example.test", "Certificate saved: ", "Add the following to the nginx config and reload it:",
	} {
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("영어 출력에 %q가 있어야 한다: err=%v\n%s", want, err, got)
		}
	}
	if hangul.MatchString(got) {
		t.Errorf("영어 실행 출력에 한글이 있다:\n%s", got)
	}

	for name, tc := range map[string]struct {
		extra []string
		want  string
	}{
		"허용 IP 밖":   {[]string{"VERIFY_CODE=403", `VERIFY_BODY={"error":"ip_not_allowed","source_ip":"198.51.100.7"}`}, "This server's IP (198.51.100.7) is not on the allowed IP list."},
		"자격 증명 오류":  {[]string{"VERIFY_CODE=401", `VERIFY_BODY={"error":"forbidden"}`}, "The username or password is wrong."},
		"CNAME 없음":  {[]string{"DIG_ANSWER="}, "No CNAME found: _acme-challenge.a.com"},
		"CNAME 다름":  {[]string{"DIG_ANSWER=other.example.net."}, "The CNAME points somewhere else: _acme-challenge.a.com -> other.example.net"},
		"sudo 경고":   {[]string{"SUDO_NEEDS_PASSWORD=1"}, "Warning: the reload command uses sudo, but this account needs a password for sudo."},
		"와일드카드":     {[]string{"DOMAIN=*.a.com"}, "Note: dropping the leading *. and continuing with a.com."},
		"웹서버 없음":    {[]string{"WEB_SERVER=iis"}, "WEB_SERVER must be one of nginx, apache, caddy, manual."},
		"수동 reload": {[]string{"WEB_SERVER=manual"}, "Manual setup: enter the reload script or command"},
	} {
		got, _ := e.run(en, append([]string{"WEB_SERVER=nginx"}, tc.extra...)...)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: 영어 메시지 %q가 없다:\n%s", name, tc.want, got)
		}
		if strings.Contains(got, "@@") {
			t.Errorf("%s: 풀리지 않은 키가 있다", name)
		}
	}

	e.removeWebServers()
	if got, err = e.run(en); err == nil || !strings.Contains(got, "No web server detected. Continuing with manual setup (manual)") || !strings.Contains(got, "RELOAD_CMD is required. Run this in a terminal or pass it as an environment variable.") {
		t.Errorf("감지 실패 영어 안내: err=%v\n%s", err, got)
	}
}
