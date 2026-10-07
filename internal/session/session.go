// Package session은 관리자 로그인 쿠키를 서명하고 검증한다. 서버에 세션을 저장하지 않으므로
// ADMIN_KEY를 바꾸면 모든 쿠키가 무효가 되고, 로그아웃은 브라우저의 쿠키만 지운다.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const (
	CookieName = "sslchk_session"
	DefaultTTL = 12 * time.Hour
)

type Signer struct {
	TTL time.Duration    // 0이면 DefaultTTL
	Now func() time.Time // nil이면 time.Now
	key [32]byte
}

// New의 서명키는 ADMIN_KEY에서 만든다. 서명키가 키 자체와 다르도록 접두어를 붙여 해시한다.
func New(adminKey string) *Signer {
	return &Signer{key: sha256.Sum256([]byte("sslchk-session:" + adminKey))}
}

func (s *Signer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Issue는 쿠키 값과 만료 시각이다. 값은 "<만료 유닉스초>.<HMAC-SHA256 base64url>"이다.
func (s *Signer) Issue() (string, time.Time) {
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	exp := s.now().Add(ttl).Truncate(time.Second)
	payload := strconv.FormatInt(exp.Unix(), 10)
	return payload + "." + s.mac(payload), exp
}

// Valid는 서명이 맞고 아직 만료되지 않은 값인지 본다.
func (s *Signer) Valid(value string) bool {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || exp <= 0 {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(payload))) {
		return false
	}
	return s.now().Unix() < exp
}

func (s *Signer) mac(payload string) string {
	m := hmac.New(sha256.New, s.key[:])
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
