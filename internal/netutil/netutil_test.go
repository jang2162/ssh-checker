package netutil_test

import (
	"testing"

	"ssl-checker/internal/netutil"
)

func TestNormalizeName(t *testing.T) {
	if got := netutil.NormalizeName(" ACME-chk.NET. "); got != "acme-chk.net" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePrefix(t *testing.T) {
	ok := map[string]string{
		"10.0.0.0/8":      "10.0.0.0/8",
		"10.1.2.3/16":     "10.1.0.0/16",
		"192.168.1.1":     "192.168.1.1/32",
		"2001:db8::1":     "2001:db8::1/128",
		"::ffff:10.0.0.1": "10.0.0.1/32",
	}
	for in, want := range ok {
		p, err := netutil.ParsePrefix(in)
		if err != nil || p.String() != want {
			t.Errorf("ParsePrefix(%q) = %v, %v; want %s", in, p, err, want)
		}
	}
	for _, bad := range []string{"10.0.0.0/33", "nope", ""} {
		if _, err := netutil.ParsePrefix(bad); err == nil {
			t.Errorf("ParsePrefix(%q) succeeded", bad)
		}
	}
}

func TestParsePrefixes(t *testing.T) {
	ps, err := netutil.ParsePrefixes("10.0.0.0/8, ,192.168.1.1")
	if err != nil || len(ps) != 2 || ps[1].String() != "192.168.1.1/32" {
		t.Fatalf("got %v, %v", ps, err)
	}
	if _, err := netutil.ParsePrefixes("10.0.0.0/8,bad"); err == nil {
		t.Fatal("bad entry accepted")
	}
	if ps, err := netutil.ParsePrefixes(""); err != nil || len(ps) != 0 {
		t.Fatalf("empty: %v, %v", ps, err)
	}
}
