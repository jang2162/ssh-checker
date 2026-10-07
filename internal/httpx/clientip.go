package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP는 요청을 보낸 클라이언트 주소를 정한다.
// X-Forwarded-For는 직접 연결한 주소가 Trusted에 속할 때만 보고, 오른쪽부터 첫 비신뢰 주소를 쓴다.
type ClientIP struct {
	Trusted []netip.Prefix
}

func (c ClientIP) From(r *http.Request) netip.Addr {
	addr := parseRemote(r.RemoteAddr)
	if !addr.IsValid() || !c.trusted(addr) {
		return addr
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return addr // 해석할 수 없는 값부터는 믿지 않는다
		}
		hop = hop.Unmap()
		if !c.trusted(hop) {
			return hop
		}
		addr = hop
	}
	return addr
}

// Proxied는 요청이 신뢰하는 프록시에서 직접 왔는지 알려 준다. 그때만 X-Forwarded-* 헤더를 믿는다.
func (c ClientIP) Proxied(r *http.Request) bool {
	a := parseRemote(r.RemoteAddr)
	return a.IsValid() && c.trusted(a)
}

func (c ClientIP) trusted(a netip.Addr) bool {
	for _, p := range c.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parseRemote(s string) netip.Addr {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}
