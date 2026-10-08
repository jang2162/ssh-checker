package adminapi_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"ssl-checker/internal/lang"
)

var hangulOut = regexp.MustCompile("[가-힣]")

// 영어 서버는 영어 스크립트, 영어 판정 코드 설명, 영어 자리표시자를 돌려준다.
func TestEnglishServerOutputs(t *testing.T) {
	f := newFixtureLang(t, lang.EN)

	rec := f.do(call{method: "GET", path: "/acme/setup.sh"})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || hangulOut.MatchString(body) || !strings.Contains(body, "Verified: acme.sh $(acme_version)") ||
		!strings.Contains(body, `ask SSLCHK_URL "ssl-checker address" "http://example.com"`) {
		t.Errorf("영어 setup.sh: %d\n%.400s", rec.Code, body)
	}

	rec = f.get("/api/codes")
	var codes struct {
		Codes []struct{ Code, Meaning string }
	}
	codes = decodeBody[struct {
		Codes []struct{ Code, Meaning string }
	}](t, rec)
	if len(codes.Codes) == 0 || hangulOut.MatchString(rec.Body.String()) || !strings.Contains(rec.Body.String(), "The certificate has expired.") {
		t.Errorf("영어 코드 설명: %s", rec.Body)
	}

	info := decodeBody[struct {
		Setup struct{ Label string }
	}](t, f.get("/api/setup"))
	if info.Setup.Label != "Setup" {
		t.Errorf("영어 라벨 = %q", info.Setup.Label)
	}
	// 주소를 알 수 없으면 영어 자리표시자를 쓴다.
	d := f.createDomain("a.com")
	rec = f.do(call{method: "POST", path: "/api/domains/" + itoa(d.ID) + "/acme", body: `{}`, bearer: true, headers: map[string]string{"Host": "bad host"}})
	created := decodeBody[struct {
		Setup struct{ Setup struct{ Cmd string } }
	}](t, rec)
	if !strings.Contains(created.Setup.Setup.Cmd, "curl -fsSL <ssl-checker address>/acme/setup.sh") {
		t.Errorf("English placeholder: %q", created.Setup.Setup.Cmd)
	}
}
