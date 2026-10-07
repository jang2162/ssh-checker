package session_test

import (
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/session"
)

const key = "admin-key-0123456789abcdef0123456789"

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestIssueAndValidate(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	v, exp := s.Issue()
	if !exp.Equal(t0.Add(12 * time.Hour)) {
		t.Fatalf("expires = %s", exp)
	}
	if !s.Valid(v) {
		t.Fatal("fresh cookie rejected")
	}
	s.Now = at(t0.Add(12*time.Hour - time.Second))
	if !s.Valid(v) {
		t.Fatal("cookie rejected before expiry")
	}
	s.Now = at(t0.Add(12 * time.Hour))
	if s.Valid(v) {
		t.Fatal("expired cookie accepted")
	}
}

func TestTamperedRejected(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	v, _ := s.Issue()
	payload, sig, _ := strings.Cut(v, ".")
	later := "9999999999"
	for _, bad := range []string{
		later + "." + sig,                      // 만료를 늘림
		payload + "." + sig[:len(sig)-1] + "A", // 서명 변조
		payload + "." + sig + "x",
		"", ".", "abc", payload, "1.2.3", "-1." + sig,
	} {
		if bad == v {
			continue
		}
		if s.Valid(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

// ADMIN_KEY를 바꾸면 이전에 발급한 쿠키는 모두 무효가 된다.
func TestOtherKeyRejected(t *testing.T) {
	a := session.New(key)
	b := session.New(key + "-rotated")
	a.Now, b.Now = at(t0), at(t0)
	v, _ := a.Issue()
	if b.Valid(v) {
		t.Fatal("cookie from old key accepted")
	}
}

func TestCustomTTL(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	s.TTL = time.Hour
	if _, exp := s.Issue(); !exp.Equal(t0.Add(time.Hour)) {
		t.Fatalf("expires = %s", exp)
	}
}
