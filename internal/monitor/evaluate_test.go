package monitor

import (
	"net/netip"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

// good은 90일짜리 인증서가 60일 남은 정상 결과다.
func good() Result {
	return Result{
		Address: "10.0.0.1:443", CheckedAt: now, TLSVersion: "TLS 1.3",
		NotBefore: now.Add(-30 * day), NotAfter: now.Add(60 * day),
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true, Serial: "01",
	}
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name   string
		mod    func(*Result)
		status Status
		codes  []string
	}{
		{"good", func(*Result) {}, StatusOK, []string{}},
		{"connect failed", func(r *Result) { *r = Result{Address: r.Address, Error: "connection refused"} }, StatusCritical, []string{CodeConnectFailed}},
		{"expired only", func(r *Result) { r.NotAfter = now.Add(-time.Hour) }, StatusCritical, []string{CodeCertExpired}},
		{"6 days left", func(r *Result) { r.NotAfter = now.Add(6 * day) }, StatusCritical, []string{CodeCertExpiresSoon}},
		{"7 days left is overdue not soon", func(r *Result) { r.NotBefore = now.Add(-83 * day); r.NotAfter = now.Add(7 * day) }, StatusWarning, []string{CodeRenewalOverdue}},
		{"31 of 90 days left", func(r *Result) { r.NotBefore = now.Add(-59 * day); r.NotAfter = now.Add(31 * day) }, StatusOK, []string{}},
		{"29 of 90 days left", func(r *Result) { r.NotBefore = now.Add(-61 * day); r.NotAfter = now.Add(29 * day) }, StatusWarning, []string{CodeRenewalOverdue}},
		{"chain invalid sorted first", func(r *Result) { r.ChainOK = false; r.NotBefore = now.Add(-61 * day); r.NotAfter = now.Add(29 * day) }, StatusCritical, []string{CodeChainInvalid, CodeRenewalOverdue}},
		{"hostname mismatch", func(r *Result) { r.HostnameOK = false }, StatusCritical, []string{CodeHostnameMismatch}},
		{"tls 1.1", func(r *Result) { r.TLSVersion = "TLS 1.1" }, StatusWarning, []string{CodeTLSOutdated}},
		{"tls 1.2", func(r *Result) { r.TLSVersion = "TLS 1.2" }, StatusOK, []string{}},
		{"rsa 2047", func(r *Result) { r.KeyAlg, r.KeyBits = "RSA", 2047 }, StatusInfo, []string{CodeWeakKey}},
		{"rsa 2048", func(r *Result) { r.KeyAlg, r.KeyBits = "RSA", 2048 }, StatusOK, []string{}},
		{"ecdsa 224", func(r *Result) { r.KeyBits = 224 }, StatusInfo, []string{CodeWeakKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := good()
			tc.mod(&r)
			st, codes := Evaluate(r, now)
			if st != tc.status || !slices.Equal(codes, tc.codes) {
				t.Fatalf("Evaluate = %s %v, want %s %v", st, codes, tc.status, tc.codes)
			}
		})
	}
}

func TestWorstAndValid(t *testing.T) {
	if Worst() != StatusUnknown || Worst(StatusOK, StatusUnknown) != StatusOK || Worst(StatusInfo, StatusCritical, StatusWarning) != StatusCritical {
		t.Fatal("Worst order wrong")
	}
	for _, s := range []Status{StatusUnknown, StatusOK, StatusInfo, StatusWarning, StatusCritical} {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if Status("bogus").Valid() || Status("").Valid() {
		t.Error("bogus status valid")
	}
}

func TestDialAddresses(t *testing.T) {
	tg := Target{Hostname: "www.a.com", Port: 8443}
	if got := tg.DialAddresses(); !slices.Equal(got, []string{"www.a.com:8443"}) {
		t.Fatal(got)
	}
	tg.IPs = []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("2001:db8::1")}
	if got := tg.DialAddresses(); !slices.Equal(got, []string{"10.0.0.1:8443", "[2001:db8::1]:8443"}) {
		t.Fatal(got)
	}
	if tg.ServerName() != "www.a.com" {
		t.Fatal(tg.ServerName())
	}
	tg.SNI = "sni.a.com"
	if tg.ServerName() != "sni.a.com" {
		t.Fatal(tg.ServerName())
	}
}
