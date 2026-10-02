package acmedns_test

import (
	"slices"
	"strings"
	"testing"

	"ssl-checker/internal/acmedns"
)

func TestValidTXT(t *testing.T) {
	ok := []string{strings.Repeat("A", 43), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJK_-0123"}
	bad := []string{"", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("+", 43), strings.Repeat("=", 43), strings.Repeat("가", 43)}
	for _, s := range ok {
		if !acmedns.ValidTXT(s) {
			t.Errorf("ValidTXT(%q) = false", s)
		}
	}
	for _, s := range bad {
		if acmedns.ValidTXT(s) {
			t.Errorf("ValidTXT(%q) = true", s)
		}
	}
}

func TestValidSubdomain(t *testing.T) {
	if !acmedns.ValidSubdomain("8e5700ea-a4bf-41c7-8a77-e990661dcc6a") {
		t.Error("valid uuid rejected")
	}
	for _, s := range []string{"", "www", "8E5700EA-A4BF-41C7-8A77-E990661DCC6A", "8e5700ea-a4bf-41c7-8a77-e990661dcc6", "8e5700ea.a4bf.41c7.8a77.e990661dcc6a"} {
		if acmedns.ValidSubdomain(s) {
			t.Errorf("ValidSubdomain(%q) = true", s)
		}
	}
}

func TestValidDomain(t *testing.T) {
	for _, s := range []string{"a.com", "sub.a.co.kr", "xn--3e0b707e.kr", "a-b.example"} {
		if !acmedns.ValidDomain(s) {
			t.Errorf("ValidDomain(%q) = false", s)
		}
	}
	for _, s := range []string{"", "com", "*.a.com", "-a.com", "a-.com", "a..com", "a.com.", "A.com", "a_b.com", strings.Repeat("a", 64) + ".com"} {
		if acmedns.ValidDomain(s) {
			t.Errorf("ValidDomain(%q) = true", s)
		}
	}
}

func TestNextValues(t *testing.T) {
	cases := []struct {
		current []string
		value   string
		want    []string
	}{
		{nil, "a", []string{"a"}},
		{[]string{"a"}, "b", []string{"a", "b"}},
		{[]string{"a", "b"}, "c", []string{"b", "c"}},
		{[]string{"a", "b"}, "a", []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := acmedns.NextValues(c.current, c.value); !slices.Equal(got, c.want) {
			t.Errorf("NextValues(%v, %q) = %v, want %v", c.current, c.value, got, c.want)
		}
	}
}
