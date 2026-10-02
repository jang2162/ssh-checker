package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/httpx"
)

func TestClientIP(t *testing.T) {
	ip := httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct, no header", "203.0.113.9:5000", nil, "203.0.113.9"},
		{"untrusted remote ignores xff", "203.0.113.9:5000", []string{"192.168.5.7"}, "203.0.113.9"},
		{"trusted proxy uses xff", "10.0.0.2:5000", []string{"192.168.5.7"}, "192.168.5.7"},
		{"rightmost untrusted wins", "10.0.0.2:5000", []string{"192.168.5.7, 203.0.113.50"}, "203.0.113.50"},
		{"skips trusted hops", "10.0.0.2:5000", []string{"192.168.5.7, 10.0.0.3"}, "192.168.5.7"},
		{"multiple header lines", "10.0.0.2:5000", []string{"192.168.5.7", "10.0.0.3"}, "192.168.5.7"},
		{"garbage hop stops at proxy", "10.0.0.2:5000", []string{"192.168.5.7, garbage"}, "10.0.0.2"},
		{"trusted proxy without header", "10.0.0.2:5000", nil, "10.0.0.2"},
		{"ipv4-mapped remote", "[::ffff:203.0.113.9]:5000", nil, "203.0.113.9"},
		{"ipv6 remote", "[2001:db8::1]:5000", nil, "2001:db8::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for _, h := range c.xff {
				r.Header.Add("X-Forwarded-For", h)
			}
			if got := ip.From(r).String(); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestKeyed(t *testing.T) {
	k := httpx.NewKeyed(time.Hour, 2)
	if !k.Allow("a") || !k.Allow("a") {
		t.Fatal("burst not allowed")
	}
	if k.Allow("a") {
		t.Fatal("third call allowed")
	}
	if !k.Exhausted("a") || k.Exhausted("b") {
		t.Fatal("Exhausted wrong")
	}
	if !k.Allow("b") {
		t.Fatal("keys not independent")
	}
}

func TestAdminAuth(t *testing.T) {
	key := strings.Repeat("k", 32)
	auth := httpx.NewAdminAuth(key, httpx.ClientIP{})
	h := auth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(remote, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/register", nil)
		r.RemoteAddr = remote
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	if rec := call("192.0.2.1:1", "Bearer "+key); rec.Code != http.StatusNoContent {
		t.Fatalf("valid key: %d", rec.Code)
	}
	for _, header := range []string{"", "Bearer wrong", key, "Basic " + key, "Bearer " + key + "x"} {
		rec := call("192.0.2.1:1", header)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"unauthorized"`) {
			t.Fatalf("header %q: %d %s", header, rec.Code, rec.Body)
		}
	}
	// 위에서 5번 실패했으므로 같은 IP는 올바른 키도 잠시 막힌다.
	if rec := call("192.0.2.1:1", "Bearer "+key); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures: %d", rec.Code)
	}
	if rec := call("192.0.2.2:1", "Bearer "+key); rec.Code != http.StatusNoContent {
		t.Fatalf("other IP blocked: %d", rec.Code)
	}
}
