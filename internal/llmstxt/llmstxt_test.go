package llmstxt

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"ssl-checker/internal/lang"
	"ssl-checker/internal/monitor"
)

func render(t *testing.T, d Data) string {
	t.Helper()
	if d.Lang == "" {
		d.Lang = lang.KO // 이 파일의 기존 시험은 한국어 본문 기준이다. 영어는 아래 TestEnglish*가 맡는다.
	}
	out, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenderFillsServerValues(t *testing.T) {
	out := render(t, Data{BaseURL: "https://ssl.example.com", Zone: "acme.example.com"})
	for _, want := range []string{
		"서버 주소: https://ssl.example.com",
		"챌린지 존: acme.example.com",
		"curl -sS -X POST https://ssl.example.com/api/domains ",
		"_acme-challenge.a.com.  CNAME  <uuid>.acme.example.com.",
		"curl -fsSL https://ssl.example.com/acme/setup.sh -o setup.sh",
		"WEB_SERVER=manual",
		"RELOAD_CMD=/usr/local/bin/reload-web.sh",
		"ACMEDNS_BASE_URL=https://ssl.example.com/acme",
		"POST https://ssl.example.com/acme/verify",
		"FORCE=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	// 스크립트에는 비밀값이 없고, 환경변수로 넘기는 방식만 안내한다.
	for _, bad := range []string{`export ACMEDNS_PASSWORD="`, "acme_sh"} {
		if strings.Contains(out, bad) {
			t.Errorf("옛 방식 안내가 남아 있다: %q", bad)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "<ssl-checker 주소>") {
		t.Errorf("unfilled template: %q", out)
	}
}

func TestRenderHasRequiredSections(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	if !strings.HasPrefix(out, "# ssl-checker\n\n> ") {
		t.Fatalf("llms.txt 형식(H1, 요약 인용문)이 아니다: %q", out[:40])
	}
	for _, h := range []string{
		"## 동작 방식", "## 관리자 인증", "## ACME 구성 절차", "### 1. 도메인 등록", "### 2. ACME 켜기",
		"### 3. CNAME 등록과 확인", "### 4. 인증서를 받을 서버에서 발급", "### 5. 확인과 모니터링 등록",
		"## 키 재발급, 허용 IP 변경, 끄기", "## 상태 판정", "## 흔한 오류와 해결", "## 모니터링 API", "## 하지 말 것",
	} {
		if !strings.Contains(out, h+"\n") {
			t.Errorf("missing section %q", h)
		}
	}
}

func TestRenderListsEveryStatusCode(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	for _, c := range monitor.Codes(lang.KO) {
		if !strings.Contains(out, "| `"+c.Code+"` | "+string(c.Status)+" | "+c.Meaning+" |") {
			t.Errorf("code %s not listed with its status and meaning", c.Code)
		}
	}
}

func TestRenderWithoutKnownBaseURLUsesPlaceholder(t *testing.T) {
	out := render(t, Data{Zone: "z.test"})
	if !strings.Contains(out, "서버 주소: <ssl-checker 주소>") {
		t.Fatal("자리표시자가 없다")
	}
}

func TestRenderNeverContainsSecrets(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	for _, bad := range []string{"password\":\"", "Bearer eyJ", "sslchk_session="} {
		if strings.Contains(out, bad) {
			t.Errorf("secret-like text %q in output", bad)
		}
	}
}

func TestHandler(t *testing.T) {
	h := Handler("z.test", lang.KO, func(r *http.Request) string { return "https://" + r.Host })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/llms.txt", nil)
	req.Host = "ssl.example.com"
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "https://ssl.example.com/api/domains") || !strings.Contains(body, "z.test") {
		t.Fatalf("body = %q", body[:200])
	}
}

var directiveRE = regexp.MustCompile(`\{\{[^}]*\}\}`)

func sortedDirectives(s string) []string {
	d := directiveRE.FindAllString(s, -1)
	sort.Strings(d)
	return d
}

// 두 언어 템플릿은 같은 구조다: 같은 {{ }} 지시어, 같은 절 개수, 같은 코드 블록 수, 같은 환경변수.
func TestTemplatesShareStructure(t *testing.T) {
	ko, en := sourceKO, sourceEN
	if strings.Join(sortedDirectives(ko), "|") != strings.Join(sortedDirectives(en), "|") {
		t.Errorf("template directives differ:\nko %v\nen %v", sortedDirectives(ko), sortedDirectives(en))
	}
	for _, marker := range []string{"\n## ", "\n### ", "\n```"} {
		if a, b := strings.Count(ko, marker), strings.Count(en, marker); a != b {
			t.Errorf("%q count differs: ko %d, en %d", strings.TrimSpace(marker), a, b)
		}
	}
	envRE := regexp.MustCompile(`\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b`)
	kos, ens := map[string]bool{}, map[string]bool{}
	for _, m := range envRE.FindAllString(ko, -1) {
		kos[m] = true
	}
	for _, m := range envRE.FindAllString(en, -1) {
		ens[m] = true
	}
	for v := range kos {
		if !ens[v] {
			t.Errorf("%s is in the Korean llms.txt but not in the English one", v)
		}
	}
}

func TestEnglishRender(t *testing.T) {
	out := render(t, Data{BaseURL: "https://ssl.example.com", Zone: "acme.example.com", Lang: lang.EN})
	if hangul := regexp.MustCompile("[가-힣]").FindString(out); hangul != "" {
		t.Errorf("English llms.txt contains Hangul near %q", hangul)
	}
	if !strings.HasPrefix(out, "# ssl-checker\n\n> ") {
		t.Fatalf("llms.txt format (H1, summary quote) broken: %.40q", out)
	}
	for _, h := range []string{
		"## How it works", "## Admin authentication", "## ACME setup procedure", "### 1. Register the domain", "### 2. Enable ACME",
		"### 3. Register and verify the CNAME", "### 4. Issue on the server that receives the certificate", "### 5. Verify and register monitoring",
		"## Rotating keys, changing allowed IPs, disabling", "## Status rules", "## Common errors and fixes", "## Monitoring API", "## Do not",
	} {
		if !strings.Contains(out, h+"\n") {
			t.Errorf("missing section %q", h)
		}
	}
	for _, want := range []string{"https://ssl.example.com/api/domains", "acme.example.com", "curl -fsSL https://ssl.example.com/acme/setup.sh -o setup.sh", "ACMEDNS_BASE_URL=https://ssl.example.com/acme"} {
		if !strings.Contains(out, want) {
			t.Errorf("English llms.txt missing %q", want)
		}
	}
	for _, c := range monitor.Codes(lang.EN) {
		if !strings.Contains(out, "| `"+c.Code+"` | "+string(c.Status)+" | "+c.Meaning+" |") {
			t.Errorf("code %s not listed with its English meaning", c.Code)
		}
	}
	// 기본 언어는 영어이고, 주소를 모르면 영어 자리표시자를 쓴다.
	def := render(t, Data{Zone: "z.test", Lang: lang.EN})
	if !strings.Contains(def, "<ssl-checker address>") || strings.Contains(def, "<ssl-checker 주소>") {
		t.Error("English placeholder missing")
	}
	if got, err := Render(Data{BaseURL: "https://x", Zone: "z"}); err != nil || strings.Contains(got, "서버 주소") {
		t.Errorf("default language should be English: %v", err)
	}
}

func TestEnglishHandler(t *testing.T) {
	h := Handler("z.test", lang.EN, func(r *http.Request) string { return "https://" + r.Host })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/llms.txt", nil)
	req.Host = "ssl.example.com"
	h.ServeHTTP(rec, req)
	if body := rec.Body.String(); rec.Code != 200 || !strings.Contains(body, "## How it works") || regexp.MustCompile("[가-힣]").MatchString(body) {
		t.Fatalf("English handler: %d %.200q", rec.Code, body)
	}
}
