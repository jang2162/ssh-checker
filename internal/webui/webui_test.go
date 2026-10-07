package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":     {Data: []byte("<!doctype html><title>app</title>")},
	"assets/app.js":  {Data: []byte("console.log(1)")},
	"assets/app.css": {Data: []byte("body{}")},
	".keep":          {Data: nil},
}

// next는 화면이 가로채지 않은 요청이 도착했다는 표시로 418을 돌려준다.
var next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServesIndexWithSecurityHeaders(t *testing.T) {
	rec := do(New(next, built), "GET", "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>app</title>") {
		t.Fatalf("/ = %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("csp = %q", csp)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", rec.Header())
	}
}

func TestIndexHTMLIsNotRedirected(t *testing.T) {
	// http.ServeFile은 /index.html을 ./로 되돌린다. 우리는 그렇게 하지 않는다.
	if rec := do(New(next, built), "GET", "/index.html"); rec.Code != 200 {
		t.Fatalf("/index.html = %d", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	for _, p := range []string{"/domains/3", "/audit", "/anything/else"} {
		rec := do(New(next, built), "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>app</title>") {
			t.Errorf("%s = %d %q", p, rec.Code, rec.Body)
		}
	}
}

func TestAssetsAreImmutableAndTyped(t *testing.T) {
	rec := do(New(next, built), "GET", "/assets/app.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("app.js = %d %q", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	if ct := do(New(next, built), "GET", "/assets/app.css").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("css content-type = %q", ct)
	}
}

func TestMissingAssetIs404NotHTML(t *testing.T) {
	rec := do(New(next, built), "GET", "/assets/missing.js")
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "<title>") {
		t.Fatalf("missing asset = %d %q", rec.Code, rec.Body)
	}
}

func TestDotFilesAreNotServed(t *testing.T) {
	rec := do(New(next, built), "GET", "/.keep")
	if !strings.Contains(rec.Body.String(), "<title>app</title>") {
		t.Fatalf("/.keep = %d %q (index로 폴백해야 한다)", rec.Code, rec.Body)
	}
}

func TestReservedPathsGoToNext(t *testing.T) {
	h := New(next, built)
	for _, p := range []string{"/api", "/api/summary", "/api/nope", "/update", "/register", "/health", "/llms.txt"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusTeapot {
			t.Errorf("GET %s = %d, want next", p, rec.Code)
		}
	}
}

func TestOtherMethodsGoToNext(t *testing.T) {
	h := New(next, built)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if rec := do(h, m, "/"); rec.Code != http.StatusTeapot {
			t.Errorf("%s / = %d, want next", m, rec.Code)
		}
	}
}

func TestHeadHasNoBody(t *testing.T) {
	rec := do(New(next, built), "HEAD", "/")
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD / = %d %q", rec.Code, rec.Body)
	}
}

func TestPlaceholderWhenNotBuilt(t *testing.T) {
	empty := fstest.MapFS{".keep": {}}
	rec := do(New(next, empty), "GET", "/")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Fatalf("placeholder = %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	// 화면 경로도 같은 안내를 보여 주고, API 경로는 그대로 next로 간다.
	if rec := do(New(next, empty), "GET", "/domains/1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/domains/1 = %d", rec.Code)
	}
	if rec := do(New(next, empty), "GET", "/api/summary"); rec.Code != http.StatusTeapot {
		t.Fatalf("/api/summary = %d", rec.Code)
	}
}

func TestEmbeddedDistBuilds(t *testing.T) {
	// 저장소에는 dist/.keep만 있어도(또는 화면을 빌드했어도) Handler가 만들어지고 응답한다.
	rec := do(Handler(next), "GET", "/")
	if rec.Code != 200 && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ = %d", rec.Code)
	}
}
