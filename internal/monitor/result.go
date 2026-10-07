package monitor

import (
	"net"
	"net/netip"
	"strconv"
	"time"
)

// Target은 점검할 TLS 엔드포인트 하나다.
type Target struct {
	ID       int64
	Hostname string
	Port     int
	SNI      string       // 비우면 Hostname
	IPs      []netip.Addr // 비우면 Hostname으로 한 번 접속한다
}

func (t Target) ServerName() string {
	if t.SNI != "" {
		return t.SNI
	}
	return t.Hostname
}

// DialAddresses는 접속할 주소다. IP 목록이 있으면 IP마다 하나, 없으면 hostname:port 하나.
// IP 주소 형식(IPv6는 [..]:port)은 저장된 결과를 대상 설정과 맞춰 볼 때도 같은 값을 쓴다.
func (t Target) DialAddresses() []string {
	if len(t.IPs) == 0 {
		return []string{net.JoinHostPort(t.Hostname, strconv.Itoa(t.Port))}
	}
	out := make([]string, len(t.IPs))
	for i, ip := range t.IPs {
		out[i] = netip.AddrPortFrom(ip, uint16(t.Port)).String()
	}
	return out
}

// Result는 접속 주소 하나의 점검 한 건이다. 인증서를 받지 못하면 NotBefore/NotAfter가 0이다.
type Result struct {
	Address    string    `json:"address"`
	CheckedAt  time.Time `json:"checked_at"`
	Status     Status    `json:"status"`
	Reasons    []string  `json:"reasons"`
	TLSVersion string    `json:"tls_version"`
	Cipher     string    `json:"cipher"`
	SubjectCN  string    `json:"subject_cn"`
	SANs       []string  `json:"sans"`
	Issuer     string    `json:"issuer"`
	Serial     string    `json:"serial"`
	NotBefore  time.Time `json:"not_before,omitzero"`
	NotAfter   time.Time `json:"not_after,omitzero"`
	KeyAlg     string    `json:"key_alg"`
	KeyBits    int       `json:"key_bits"`
	ChainOK    bool      `json:"chain_ok"`
	HostnameOK bool      `json:"hostname_ok"`
	Error      string    `json:"error"`
}

func (r Result) HasCert() bool { return !r.NotAfter.IsZero() }
