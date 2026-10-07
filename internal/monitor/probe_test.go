package monitor

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func probeOne(t *testing.T, p *Prober, tg Target) Result {
	t.Helper()
	rs := p.Probe(context.Background(), tg)
	if len(rs) != 1 {
		t.Fatalf("got %d results", len(rs))
	}
	return rs[0]
}

func expectCodes(t *testing.T, r Result, status Status, codes ...string) {
	t.Helper()
	if codes == nil {
		codes = []string{}
	}
	if r.Status != status || !slices.Equal(r.Reasons, codes) {
		t.Fatalf("status %s %v (err=%q), want %s %v", r.Status, r.Reasons, r.Error, status, codes)
	}
}

func TestProbeTrusted(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}, serial: 0xab}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusOK)
	if r.Address != fmt.Sprintf("127.0.0.1:%d", port) || r.Serial != "ab" || r.Issuer != "Test CA" || r.SubjectCN != "a.test" ||
		!slices.Equal(r.SANs, []string{"a.test"}) || r.KeyAlg != "ECDSA" || r.KeyBits != 256 ||
		r.TLSVersion != "TLS 1.3" || r.Cipher == "" || !r.ChainOK || !r.HostnameOK || r.CheckedAt.IsZero() {
		t.Fatalf("result = %+v", r)
	}
}

func TestProbeSelfSigned(t *testing.T) {
	port := serveTLS(t, selfSigned(t, "a.test"), nil)
	r := probeOne(t, &Prober{Roots: newCA(t).pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeChainInvalid)
	if !r.HostnameOK {
		t.Fatal("hostname should still match")
	}
}

func TestProbeHostnameMismatch(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"b.test"}}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeHostnameMismatch)
}

func TestProbeUsesSNI(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"sni.test"}}), nil)
	tg := target(port, "a.test", "127.0.0.1")
	tg.SNI = "sni.test"
	expectCodes(t, probeOne(t, &Prober{Roots: ca.pool}, tg), StatusOK)
}

// 만료된 인증서는 체인 문제로 오판하지 않고 CERT_EXPIRED 하나만 붙어야 한다.
func TestProbeExpired(t *testing.T) {
	ca := newCA(t)
	cert := ca.issue(t, leafOpts{dns: []string{"a.test"}, notBefore: time.Now().Add(-90 * 24 * time.Hour), notAfter: time.Now().Add(-time.Hour)})
	port := serveTLS(t, cert, nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeCertExpired)
	if !r.ChainOK {
		t.Fatal("chain should be judged on trust, not expiry")
	}
}

func TestProbeConnectRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	r := probeOne(t, &Prober{}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeConnectFailed)
	if r.Error == "" || r.HasCert() {
		t.Fatalf("result = %+v", r)
	}
}

func TestProbeHandshakeTimeout(t *testing.T) {
	port := hangingPort(t)
	start := time.Now()
	r := probeOne(t, &Prober{Timeout: 300 * time.Millisecond}, target(port, "a.test", "127.0.0.1"))
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
	expectCodes(t, r, StatusCritical, CodeConnectFailed)
}

func TestProbeIPv6(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback")
	}
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), ln)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "::1"))
	expectCodes(t, r, StatusOK)
	if r.Address != fmt.Sprintf("[::1]:%d", port) {
		t.Fatalf("address = %s", r.Address)
	}
}

// IP 목록이 없으면 호스트명으로 한 번 접속하고, 실제로 붙은 주소를 기록한다.
func TestProbeHostnameMode(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"localhost"}}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "localhost"))
	expectCodes(t, r, StatusOK)
	if !strings.HasSuffix(r.Address, fmt.Sprintf(":%d", port)) || strings.HasPrefix(r.Address, "localhost") {
		t.Fatalf("address = %s", r.Address)
	}
}

func TestProbeMultipleIPs(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), nil)
	rs := (&Prober{Roots: ca.pool, Timeout: time.Second}).Probe(context.Background(), target(port, "a.test", "127.0.0.1", "127.0.0.2"))
	if len(rs) != 2 || rs[0].Address != fmt.Sprintf("127.0.0.1:%d", port) || rs[1].Address != fmt.Sprintf("127.0.0.2:%d", port) {
		t.Fatalf("results = %+v", rs)
	}
}

// 접속이 안 되는 주소가 여러 개여도 점검 한 번이 타임아웃 한 번 정도에 끝나야 한다.
func TestProbeAddressesConcurrently(t *testing.T) {
	port := hangingPort(t)
	tg := target(port, "a.test", "127.0.0.1", "127.0.0.1", "127.0.0.1")
	start := time.Now()
	rs := (&Prober{Timeout: 300 * time.Millisecond}).Probe(context.Background(), tg)
	if elapsed := time.Since(start); elapsed > 800*time.Millisecond {
		t.Fatalf("Probe took %s; addresses were not probed concurrently", elapsed)
	}
	if len(rs) != 3 {
		t.Fatalf("got %d results", len(rs))
	}
	for _, r := range rs {
		expectCodes(t, r, StatusCritical, CodeConnectFailed)
	}
}
