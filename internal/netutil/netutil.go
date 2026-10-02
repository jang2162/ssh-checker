// Package netutil은 DNS 이름과 IP 대역을 다루는 작은 도우미를 모은다.
package netutil

import (
	"fmt"
	"net/netip"
	"strings"
)

// NormalizeName은 DNS 이름을 소문자로 바꾸고 앞뒤 공백과 끝의 점을 없앤다.
func NormalizeName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// ParsePrefix는 CIDR 또는 단일 IP를 대역으로 읽는다. 단일 IP는 /32 또는 /128이 된다.
func ParsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q", s)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP %q", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// ParsePrefixes는 쉼표로 구분한 CIDR·IP 목록을 읽는다. 빈 항목은 건너뛴다.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := ParsePrefix(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
