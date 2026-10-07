package llmstxt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssl-checker/internal/monitor"
)

func render(t *testing.T, d Data) string {
	t.Helper()
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
		`export ACMEDNS_BASE_URL="https://ssl.example.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
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
	for _, c := range monitor.Codes() {
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
	h := Handler("z.test", func(r *http.Request) string { return "https://" + r.Host })
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
