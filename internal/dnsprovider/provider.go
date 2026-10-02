// Package dnsprovider는 챌린지 TXT를 기록하는 DNS 공급자 계약을 정의한다.
// 구현체는 기록만 하고, 값 2개 유지·검증·감사 로그 같은 정책은 앱 코어가 맡는다.
package dnsprovider

import (
	"context"
	"errors"
	"fmt"
)

type Provider interface {
	// SetTXT는 fqdn의 TXT 값 집합을 values와 같게 맞춘다 (최대 2개).
	// 같은 입력으로 여러 번 호출해도 결과가 같아야 한다. values가 비면 모두 지운다.
	SetTXT(ctx context.Context, fqdn string, values []string, ttl int) error

	// DeleteTXT는 fqdn의 TXT 레코드를 모두 지운다. 없으면 성공으로 본다.
	DeleteTXT(ctx context.Context, fqdn string) error

	// ListTXT는 존 안의 TXT 레코드 목록이다. DB와 대조해 고아 레코드를 정리할 때 쓴다.
	ListTXT(ctx context.Context, zone string) ([]TXTRecord, error)

	// Check는 자격증명과 존 접근이 정상인지 확인한다. /health에 반영한다.
	Check(ctx context.Context) error

	// Capabilities는 최소 TTL, 존당 레코드 수 제한 같은 공급자 특성이다.
	Capabilities() Capabilities
}

// TXTRecord의 FQDN은 소문자이고 끝에 점이 없다. ProviderRef는 공급자별 레코드 ID로, 코어는 의존하지 않는다.
type TXTRecord struct {
	FQDN        string
	Value       string
	ProviderRef string
}

type Capabilities struct {
	MinTTL            int
	MaxRecordsPerZone int // 0이면 알 수 없음
}

// ErrorKind는 재시도하면 될 일시 오류와 사람이 고쳐야 할 설정 오류를 나눈다.
type ErrorKind int

const (
	KindTransient ErrorKind = iota + 1 // 요청 제한, 시간 초과, 5xx
	KindConfig                         // 권한, 존 없음, 잘못된 요청
)

func (k ErrorKind) String() string {
	switch k {
	case KindTransient:
		return "transient"
	case KindConfig:
		return "config"
	}
	return "unknown"
}

type Error struct {
	Kind ErrorKind
	Op   string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("dns %s (%s): %v", e.Op, e.Kind, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// KindOf는 err 안의 *Error 종류를 돌려준다. 분류되지 않은 오류는 일시 오류로 본다.
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindTransient
}

// Dedupe는 순서를 지키며 중복과 빈 문자열을 없앤다.
func Dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
