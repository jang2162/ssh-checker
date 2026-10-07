package adminapi_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/adminapi"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/session"
)

func sessionCookie(t *testing.T, rec interface{ Result() *http.Response }) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestLoginLogout(t *testing.T) {
	f := newFixture(t)
	if got := decodeBody[map[string]bool](t, f.get("/api/session")); got["admin"] {
		t.Fatal("anonymous reported as admin")
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`}), http.StatusUnauthorized, "unauthorized")

	rec := f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`})
	if rec.Code != http.StatusOK || !decodeBody[map[string]bool](t, rec)["admin"] {
		t.Fatalf("login = %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge <= 0 || strings.Contains(c.Value, adminKey) {
		t.Fatalf("cookie = %+v", c)
	}
	if got := decodeBody[map[string]bool](t, f.do(call{method: "GET", path: "/api/session", cookie: c})); !got["admin"] {
		t.Fatal("cookie not recognized")
	}
	if rec := f.do(call{method: "GET", path: "/api/audit", cookie: c}); rec.Code != http.StatusOK {
		t.Fatalf("audit with cookie = %d", rec.Code)
	}

	out := f.do(call{method: "DELETE", path: "/api/session", cookie: c})
	if out.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", out.Code)
	}
	if cleared := sessionCookie(t, out); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("logout cookie = %+v", cleared)
	}
}

func TestSecureCookieFollowsPublicURL(t *testing.T) {
	f := newFixture(t)
	login := func(c call) *http.Cookie {
		c.method, c.path, c.body = "POST", "/api/session", `{"key":"`+adminKey+`"}`
		return sessionCookie(t, f.do(c))
	}
	if c := login(call{}); c.Secure {
		t.Fatal("plain http request got Secure cookie")
	}
	proxied := call{remote: "10.0.0.5:4000", headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "ssl.example.com"}}
	if c := login(proxied); !c.Secure {
		t.Fatal("https via trusted proxy got non-Secure cookie")
	}
}

func TestAdminRequired(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.get("/api/audit"), http.StatusUnauthorized, "unauthorized")
	bad := f.do(call{method: "GET", path: "/api/audit", headers: map[string]string{"Authorization": "Bearer nope"}})
	expectError(t, bad, http.StatusUnauthorized, "unauthorized")
	forged := &http.Cookie{Name: session.CookieName, Value: "9999999999.forged"}
	expectError(t, f.do(call{method: "GET", path: "/api/audit", cookie: forged}), http.StatusUnauthorized, "unauthorized")
	if rec := f.do(call{method: "GET", path: "/api/audit", bearer: true}); rec.Code != http.StatusOK {
		t.Fatalf("bearer = %d %s", rec.Code, rec.Body)
	}
}

// 틀린 키는 IP별로 5번까지 받고 그다음은 429다. 로그인과 Bearer가 같은 한도를 쓴다.
func TestWrongKeyLockout(t *testing.T) {
	f := newFixture(t)
	for range 3 {
		f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`})
	}
	for range 2 {
		f.do(call{method: "GET", path: "/api/audit", headers: map[string]string{"Authorization": "Bearer wrong"}})
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`}), http.StatusTooManyRequests, "too_many_attempts")
	expectError(t, f.do(call{method: "GET", path: "/api/audit", bearer: true}), http.StatusTooManyRequests, "too_many_attempts")
	// 다른 IP는 영향을 받지 않는다.
	if rec := f.do(call{method: "GET", path: "/api/audit", bearer: true, remote: "198.51.100.7:1"}); rec.Code != http.StatusOK {
		t.Fatalf("other ip = %d", rec.Code)
	}
}

// 잠긴 IP에서도 로그인한 관리자의 쿠키는 통과하고, 올바른 Bearer는 여전히 막힌다.
func TestValidCookieBypassesLockout(t *testing.T) {
	f := newFixture(t)
	c := sessionCookie(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`}))
	for range 5 {
		f.do(call{method: "GET", path: "/api/audit", headers: map[string]string{"Authorization": "Bearer wrong"}})
	}
	expectError(t, f.do(call{method: "GET", path: "/api/audit", bearer: true}), http.StatusTooManyRequests, "too_many_attempts")
	if rec := f.do(call{method: "GET", path: "/api/audit", cookie: c}); rec.Code != http.StatusOK {
		t.Fatalf("cookie from locked ip = %d %s", rec.Code, rec.Body)
	}
}

// /register(httpx.AdminAuth)와 /api가 한 실패 한도를 나눠 쓴다.
func TestSharedFailureBucket(t *testing.T) {
	failures := httpx.NewKeyed(12*time.Second, 5)
	api := &adminapi.API{Auth: adminapi.NewAuthShared(adminKey, failures), IP: httpx.ClientIP{}, Log: slog.New(slog.DiscardHandler)}
	mux := http.NewServeMux()
	api.Register(mux) // 잠긴 뒤에는 저장소에 닿기 전에 429로 끝나므로 Store는 필요 없다
	legacyAuth := httpx.NewAdminAuthShared(adminKey, httpx.ClientIP{}, failures)
	legacyAuth.Log = slog.New(slog.DiscardHandler)
	legacy := legacyAuth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	do := func(h http.Handler, bearer string) int {
		r := httptest.NewRequest("GET", "/api/audit", nil)
		r.RemoteAddr = "192.0.2.1:1"
		r.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	for range 3 {
		do(mux, "wrong")
	}
	for range 2 {
		do(legacy, "wrong")
	}
	if got := do(mux, adminKey); got != http.StatusTooManyRequests {
		t.Fatalf("api after 5 shared failures = %d", got)
	}
	if got := do(legacy, adminKey); got != http.StatusTooManyRequests {
		t.Fatalf("legacy after 5 shared failures = %d", got)
	}
}

// 다른 사이트의 폼 전송(쿠키는 붙어 옴)으로 쓰기 요청을 만들 수 없어야 한다.
func TestWriteRequiresJSON(t *testing.T) {
	f := newFixture(t)
	c := sessionCookie(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`}))
	for _, ct := range []string{"application/x-www-form-urlencoded", "text/plain", "multipart/form-data; boundary=x", "-"} {
		rec := f.do(call{method: "DELETE", path: "/api/session", cookie: c, contentType: ct})
		expectError(t, rec, http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	if rec := f.do(call{method: "DELETE", path: "/api/session", cookie: c, contentType: "application/json; charset=utf-8"}); rec.Code != http.StatusNoContent {
		t.Fatalf("json with charset = %d", rec.Code)
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"x","extra":1}`}), http.StatusBadRequest, "malformed_json")
	big := `{"key":"` + strings.Repeat("a", 70<<10) + `"}`
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: big}), http.StatusBadRequest, "malformed_json")
}

func TestAuditListing(t *testing.T) {
	f := newFixture(t)
	f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`})
	f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`})
	type entry struct {
		ID       int64  `json:"id"`
		Actor    string `json:"actor"`
		Action   string `json:"action"`
		SourceIP string `json:"source_ip"`
	}
	resp := decodeBody[struct{ Entries []entry }](t, f.admin("GET", "/api/audit", ""))
	if len(resp.Entries) != 2 || resp.Entries[0].Action != "login" || resp.Entries[0].Actor != "admin" ||
		resp.Entries[1].Action != "login_denied" || resp.Entries[1].Actor != "anonymous" || resp.Entries[0].SourceIP == "" {
		t.Fatalf("entries = %+v", resp.Entries)
	}
	page := decodeBody[struct{ Entries []entry }](t, f.admin("GET", "/api/audit?limit=1&before_id="+itoa(resp.Entries[0].ID), ""))
	if len(page.Entries) != 1 || page.Entries[0].Action != "login_denied" {
		t.Fatalf("page = %+v", page.Entries)
	}
	expectError(t, f.admin("GET", "/api/audit?limit=0", ""), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.admin("GET", "/api/audit?before_id=x", ""), http.StatusBadRequest, "invalid_before_id")
}

// GET /api/session은 Bearer를 보지 않는다: 키 추측 수단이 되지 않고 실패 횟수도 세지 않는다.
func TestSessionStatusIgnoresBearer(t *testing.T) {
	f := newFixture(t)
	for range 6 {
		rec := f.do(call{method: "GET", path: "/api/session", headers: map[string]string{"Authorization": "Bearer wrong"}})
		if got := decodeBody[map[string]bool](t, rec); got["admin"] {
			t.Fatal("wrong bearer reported as admin")
		}
	}
	if rec := f.do(call{method: "GET", path: "/api/audit", bearer: true}); rec.Code != http.StatusOK {
		t.Fatalf("audit after status probes = %d", rec.Code)
	}
	if got := decodeBody[map[string]bool](t, f.do(call{method: "GET", path: "/api/session", bearer: true})); got["admin"] {
		t.Fatal("bearer without cookie reported as admin")
	}
}

// 형식이 이상한 Host/X-Forwarded-Host는 공개 주소에 쓰지 않는다.
func TestPublicURLRejectsOddHosts(t *testing.T) {
	f := newFixture(t)
	cfg := func(c call) string {
		c.method, c.path = "GET", "/api/config"
		return decodeBody[map[string]string](t, f.do(c))["public_url"]
	}
	fwd := func(host string) call {
		return call{remote: "10.0.0.5:4000", headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": host}}
	}
	if got := cfg(fwd("evil.example/x")); got != "https://example.com" {
		t.Fatalf("path in forwarded host: %q", got)
	}
	if got := cfg(fwd("a@b.example")); got != "https://example.com" {
		t.Fatalf("userinfo in forwarded host: %q", got)
	}
	if got := cfg(fwd("[2001:db8::1]:8443")); got != "https://[2001:db8::1]:8443" {
		t.Fatalf("ipv6 host: %q", got)
	}
	if got := cfg(call{headers: map[string]string{"Host": "bad host"}}); got != "" {
		t.Fatalf("invalid Host header: %q", got)
	}
}
