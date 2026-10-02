//go:build compat

// Package compat은 같은 /update 요청을 원본 acme-dns와 ssl-checker에 보내 응답이 같은지 확인한다.
// scripts/compat.sh가 두 서버를 띄운 뒤 실행한다.
package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	origURL  = envOr("COMPAT_ACMEDNS_URL", "http://127.0.0.1:18080")
	oursURL  = envOr("COMPAT_SSLCHK_URL", "http://127.0.0.1:18081")
	adminKey = envOr("COMPAT_ADMIN_KEY", "compat-admin-key-0123456789abcdef0123")
)

type creds struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Subdomain string `json:"subdomain"`
}

type reply struct {
	Status int
	Body   any
}

func post(t *testing.T, url string, headers map[string]string, body string) reply {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &parsed); err != nil {
		parsed = map[string]any{"_raw": string(raw)}
	}
	return reply{Status: resp.StatusCode, Body: parsed}
}

func register(t *testing.T, url string, headers map[string]string, body string) creds {
	t.Helper()
	r := post(t, url+"/register", headers, body)
	if r.Status != http.StatusCreated {
		t.Fatalf("register %s: %d %v", url, r.Status, r.Body)
	}
	raw, _ := json.Marshal(r.Body)
	var c creds
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUpdateMatchesAcmeDNS(t *testing.T) {
	orig := register(t, origURL, nil, "")
	ours := register(t, oursURL, map[string]string{"Authorization": "Bearer " + adminKey},
		fmt.Sprintf(`{"domain":"compat-%d.example"}`, time.Now().UnixNano()))

	valid := strings.Repeat("A", 43)
	auth := func(c creds) map[string]string {
		return map[string]string{"X-Api-User": c.Username, "X-Api-Key": c.Password, "Content-Type": "application/json"}
	}
	body := func(sub, txt string) string { return fmt.Sprintf(`{"subdomain":%q,"txt":%q}`, sub, txt) }
	stranger := uuid.NewString()

	cases := []struct {
		name  string
		build func(c creds) (map[string]string, string)
	}{
		{"valid", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, valid) }},
		{"second value", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("B", 43)) }},
		{"third value", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("C", 43)) }},
		{"same value again", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, valid) }},
		{"wrong key", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-Key"] = "wrong"
			return h, body(c.Subdomain, valid)
		}},
		{"unknown user", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-User"] = stranger
			return h, body(c.Subdomain, valid)
		}},
		{"non-uuid user", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-User"] = "abc"
			return h, body(c.Subdomain, valid)
		}},
		{"missing headers", func(c creds) (map[string]string, string) { return nil, body(c.Subdomain, valid) }},
		{"subdomain mismatch", func(c creds) (map[string]string, string) { return auth(c), body(stranger, valid) }},
		{"malformed json", func(c creds) (map[string]string, string) { return auth(c), "{not json" }},
		{"empty body", func(c creds) (map[string]string, string) { return auth(c), "" }},
		{"txt 42 chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("A", 42)) }},
		{"txt 44 chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("A", 44)) }},
		{"txt bad chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("+", 43)) }},
		{"txt empty", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h1, b1 := c.build(orig)
			h2, b2 := c.build(ours)
			r1 := post(t, origURL+"/update", h1, b1)
			r2 := post(t, oursURL+"/update", h2, b2)
			if r1.Status != r2.Status || !reflect.DeepEqual(r1.Body, r2.Body) {
				t.Errorf("acme-dns: %d %v\nssl-checker: %d %v", r1.Status, r1.Body, r2.Status, r2.Body)
			}
		})
	}
}
