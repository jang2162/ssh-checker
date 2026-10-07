package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/config"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
)

func serveHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		DNSProvider: "memory", ChallengeZone: "acme.test", TXTTTL: 60, AdminKey: strings.Repeat("k", 32),
		CheckInterval: time.Hour, DNSResolver: "127.0.0.1:1",
	}
	return newServeHandler(cfg, dbtest.New(t), memory.New())
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServeHandlerKeepsAPIPathsAheadOfTheUI(t *testing.T) {
	h := serveHandler(t)

	// 화면이 가로채지 않는다: 예전과 같은 405, JSON 404.
	rec := get(h, "GET", "/update")
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Header().Get("Allow"), "POST") {
		t.Fatalf("GET /update = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = get(h, "GET", "/api/nope")
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET /api/nope = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(h, "POST", "/register"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /register without key = %d", rec.Code)
	}

	rec = get(h, "GET", "/api/summary")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"domains":0`) {
		t.Fatalf("GET /api/summary = %d %q", rec.Code, rec.Body)
	}
}

func TestServeHandlerServesLLMSTxt(t *testing.T) {
	rec := get(serveHandler(t), "GET", "/llms.txt")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("GET /llms.txt = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if body := rec.Body.String(); !strings.Contains(body, "챌린지 존: acme.test") || !strings.Contains(body, "http://example.com/api/domains") {
		t.Fatalf("body = %q", body[:300])
	}
}

func TestServeHandlerServesTheUI(t *testing.T) {
	h := serveHandler(t)
	for _, path := range []string{"/", "/domains/1", "/audit"} {
		rec := get(h, "GET", path)
		// 화면을 빌드했으면 200, 안 했으면 안내 페이지(503)다. 어느 쪽이든 HTML이다.
		if (rec.Code != 200 && rec.Code != http.StatusServiceUnavailable) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s = %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
