package cnamecheck_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
)

const want = "u1.acme.example.com"

func TestCheck(t *testing.T) {
	resolver := cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.ok.com.":       {CNAME: "u1.acme.example.com."},
		"_acme-challenge.case.com.":     {CNAME: "U1.ACME.Example.COM."},
		"_acme-challenge.other.com.":    {CNAME: "u2.acme.example.com."},
		"_acme-challenge.nx.com.":       {RCode: dnsmessage.RCodeNameError},
		"_acme-challenge.servfail.com.": {RCode: dnsmessage.RCodeServerFailure},
		"_acme-challenge.drop.com.":     {Drop: true},
	})
	c := &cnamecheck.Checker{Resolver: resolver, Timeout: 300 * time.Millisecond}
	cases := []struct {
		name, domain string
		status       cnamecheck.Status
		value        string
	}{
		{"ok", "ok.com", cnamecheck.StatusOK, want},
		{"case-and-dot", "case.com", cnamecheck.StatusOK, want},
		{"mismatch", "other.com", cnamecheck.StatusMismatch, "u2.acme.example.com"},
		{"no-record", "empty.com", cnamecheck.StatusMissing, ""},
		{"nxdomain", "nx.com", cnamecheck.StatusMissing, ""},
		{"servfail", "servfail.com", cnamecheck.StatusError, ""},
		{"timeout", "drop.com", cnamecheck.StatusError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			got := c.Check(context.Background(), tc.domain, want+".")
			if got.Status != tc.status || got.Value != tc.value {
				t.Fatalf("Check(%s) = %+v, want %s %q", tc.domain, got, tc.status, tc.value)
			}
			if (tc.status == cnamecheck.StatusError) != (got.Err != nil) {
				t.Fatalf("Err = %v", got.Err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatalf("took %s", time.Since(start))
			}
		})
	}
}

func TestChallengeName(t *testing.T) {
	if got := cnamecheck.ChallengeName("a.com"); got != "_acme-challenge.a.com" {
		t.Fatal(got)
	}
}
