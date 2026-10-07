package monitor

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"time"
)

const DefaultTimeout = 10 * time.Second

// Prober는 TLS로 접속해 인증서를 받고 따로 검증한다.
type Prober struct {
	Roots   *x509.CertPool   // nil이면 시스템 신뢰 저장소
	Timeout time.Duration    // 주소 하나의 접속+핸드셰이크 상한. 0이면 DefaultTimeout
	Now     func() time.Time // nil이면 time.Now
}

// Probe는 대상의 접속 주소마다 결과 하나를 돌려준다. 판정(Status, Reasons)까지 채운다.
func (p *Prober) Probe(ctx context.Context, t Target) []Result {
	addrs := t.DialAddresses()
	out := make([]Result, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, p.probeAddr(ctx, t, addr))
	}
	return out
}

func (p *Prober) probeAddr(ctx context.Context, t Target, addr string) Result {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	r := Result{Address: addr, CheckedAt: now.UTC()}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := tls.Dialer{Config: &tls.Config{
		ServerName: t.ServerName(),
		// 잘못된 인증서도 받아서 보여 줘야 하므로 핸드셰이크에서는 검증하지 않고 아래에서 따로 검증한다.
		InsecureSkipVerify: true, //nolint:gosec
		// 낡은 서버에도 붙어서 TLS_OUTDATED로 표시한다.
		MinVersion: tls.VersionTLS10,
	}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		r.Error = err.Error()
		r.Status, r.Reasons = Evaluate(r, now)
		return r
	}
	defer conn.Close()
	tc := conn.(*tls.Conn)
	if len(t.IPs) == 0 {
		r.Address = tc.RemoteAddr().String() // 호스트명으로 접속하면 실제로 붙은 주소를 남긴다
	}
	fill(&r, tc.ConnectionState(), t.ServerName(), p.Roots, now)
	r.Status, r.Reasons = Evaluate(r, now)
	return r
}

func fill(r *Result, st tls.ConnectionState, name string, roots *x509.CertPool, now time.Time) {
	r.TLSVersion = tls.VersionName(st.Version)
	r.Cipher = tls.CipherSuiteName(st.CipherSuite)
	if len(st.PeerCertificates) == 0 {
		r.Error = "server sent no certificate"
		return
	}
	leaf := st.PeerCertificates[0]
	r.SubjectCN = leaf.Subject.CommonName
	r.SANs = append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		r.SANs = append(r.SANs, ip.String())
	}
	r.Issuer = leaf.Issuer.CommonName
	if r.Issuer == "" && len(leaf.Issuer.Organization) > 0 {
		r.Issuer = leaf.Issuer.Organization[0]
	}
	r.Serial = leaf.SerialNumber.Text(16)
	r.NotBefore, r.NotAfter = leaf.NotBefore.UTC(), leaf.NotAfter.UTC()
	r.KeyAlg, r.KeyBits = keyInfo(leaf.PublicKey)

	inter := x509.NewCertPool()
	for _, c := range st.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	// 만료는 CERT_EXPIRED로 따로 판정하므로, 체인은 만료 직전 시점 기준으로 신뢰 여부만 본다.
	at := now
	if !leaf.NotAfter.After(now) {
		at = leaf.NotAfter.Add(-time.Minute)
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: at})
	r.ChainOK = err == nil
	r.HostnameOK = leaf.VerifyHostname(name) == nil
}

func keyInfo(pub any) (string, int) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "RSA", k.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", k.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	}
	return "unknown", 0
}
