package adminapi

import (
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"

	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const (
	maxMemoLen   = 500
	maxTargetIPs = 16
)

var labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validMemo(s string) bool { return utf8.RuneCountInString(s) <= maxMemoLen }

// validHostname은 점검할 호스트 이름이다. 내부 이름(한 단어)과 IP 리터럴도 받고, 와일드카드는 받지 않는다.
func validHostname(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	if s == "" || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

// validSNI는 TLS SNI로 보낼 이름이다. SNI에는 IP를 쓸 수 없다.
func validSNI(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return false
	}
	return validHostname(s)
}

// parseIPs는 공백을 지우고, IPv4-mapped IPv6를 IPv4로 바꾸고, 중복을 뺀다. 존(%eth0)이 붙은 주소는 받지 않는다.
func parseIPs(ss []string) ([]netip.Addr, bool) {
	out := make([]netip.Addr, 0, len(ss))
	for _, s := range ss {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || a.Zone() != "" {
			return nil, false
		}
		a = a.Unmap()
		if !slicesContains(out, a) {
			out = append(out, a)
		}
	}
	return out, len(out) <= maxTargetIPs
}

func slicesContains(as []netip.Addr, a netip.Addr) bool {
	for _, x := range as {
		if x == a {
			return true
		}
	}
	return false
}

// targetRequest는 대상 추가·수정 본문이다. 수정에서는 보낸 필드만 바꾼다.
type targetRequest struct {
	Hostname *string   `json:"hostname"`
	Port     *int      `json:"port"`
	SNI      *string   `json:"sni"`
	IPs      *[]string `json:"ips"`
	Memo     *string   `json:"memo"`
}

// apply는 요청 값을 base에 덮어쓰고 검사한다.
func (req targetRequest) apply(base store.TargetFields) (store.TargetFields, bool) {
	f := base
	if req.Hostname != nil {
		f.Hostname = netutil.NormalizeName(*req.Hostname)
	}
	if req.Port != nil {
		f.Port = *req.Port
	}
	if req.SNI != nil {
		f.SNI = netutil.NormalizeName(*req.SNI)
	}
	if req.IPs != nil {
		ips, ok := parseIPs(*req.IPs)
		if !ok {
			return f, false
		}
		f.IPs = ips
	}
	if req.Memo != nil {
		f.Memo = *req.Memo
	}
	ok := validHostname(f.Hostname) && f.Port >= 1 && f.Port <= 65535 &&
		(f.SNI == "" || validSNI(f.SNI)) && validMemo(f.Memo)
	return f, ok
}

// connectionChanged는 접속 결과에 영향을 주는 값이 바뀌었는지 본다(메모만 바뀌면 다시 점검하지 않는다).
func connectionChanged(a, b store.TargetFields) bool {
	if a.Hostname != b.Hostname || a.Port != b.Port || a.SNI != b.SNI || len(a.IPs) != len(b.IPs) {
		return true
	}
	for i := range a.IPs {
		if a.IPs[i] != b.IPs[i] {
			return true
		}
	}
	return false
}
