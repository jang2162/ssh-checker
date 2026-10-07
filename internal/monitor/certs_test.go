package monitor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-200 * 24 * time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

type leafOpts struct {
	dns       []string
	notBefore time.Time // 0이면 1시간 전
	notAfter  time.Time // 0이면 60일 뒤
	serial    int64     // 0이면 2
}

func (ca *testCA) issue(t *testing.T, o leafOpts) tls.Certificate {
	t.Helper()
	return makeLeaf(t, o, ca.cert, ca.key)
}

// selfSigned는 어떤 CA에도 묶이지 않은 인증서다.
func selfSigned(t *testing.T, dns ...string) tls.Certificate {
	t.Helper()
	return makeLeaf(t, leafOpts{dns: dns}, nil, nil)
}

func makeLeaf(t *testing.T, o leafOpts, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(60 * 24 * time.Hour)
	}
	if o.serial == 0 {
		o.serial = 2
	}
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(o.serial), Subject: pkix.Name{CommonName: o.dns[0]}, DNSNames: o.dns,
		NotBefore: o.notBefore, NotAfter: o.notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	signer := key
	if parent == nil {
		parent = tmpl
	} else {
		signer = parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serveTLS는 cert로 TLS를 받는 서버를 ln(nil이면 127.0.0.1의 빈 포트)에 띄우고 포트를 돌려준다.
func serveTLS(t *testing.T, cert tls.Certificate, ln net.Listener) int {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	if ln != nil {
		srv.Listener.Close()
		srv.Listener = ln
	}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// hangingPort는 TCP 연결은 받지만 아무 응답도 하지 않는 서버의 포트다.
func hangingPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
		for _, c := range conns {
			c.Close()
		}
	})
	return ln.Addr().(*net.TCPAddr).Port
}

func target(port int, host string, ips ...string) Target {
	tg := Target{ID: 1, Hostname: host, Port: port}
	for _, ip := range ips {
		tg.IPs = append(tg.IPs, netip.MustParseAddr(ip))
	}
	return tg
}
