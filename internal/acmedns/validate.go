// Package acmedns는 acme.sh의 dns_acmedns 플러그인이 호출하는 acme-dns 호환 API를 구현한다.
package acmedns

import (
	"regexp"
	"slices"
	"strings"
)

const (
	txtLen    = 43 // SHA-256을 base64url로 인코딩한 길이
	maxValues = 2  // a.com과 *.a.com을 함께 발급하면 값 2개가 동시에 필요하다
)

var (
	subdomainRE = regexp.MustCompile(`^[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}$`)
	labelRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ValidTXT는 ACME DNS-01 검증 값 형식(43자, [A-Za-z0-9_-])인지 본다.
func ValidTXT(s string) bool {
	if len(s) != txtLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ValidSubdomain은 앱이 발급한 서브도메인(소문자 UUID) 형식인지 본다.
func ValidSubdomain(s string) bool { return subdomainRE.MatchString(s) }

// ValidDomain은 등록할 도메인 이름(예: a.com)을 검사한다. 소문자로 정규화된 이름만 받고 와일드카드는 받지 않는다.
func ValidDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

// NextValues는 최근 값 2개 유지 규칙을 적용한다. current는 오래된 순이다.
// 이미 있는 값이면 그대로, 3번째 값이면 가장 오래된 값을 밀어낸다.
func NextValues(current []string, value string) []string {
	if slices.Contains(current, value) {
		return slices.Clone(current)
	}
	next := append(slices.Clone(current), value)
	if len(next) > maxValues {
		next = next[len(next)-maxValues:]
	}
	return next
}
