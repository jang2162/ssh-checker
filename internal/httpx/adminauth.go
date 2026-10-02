package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// AdminAuth는 Authorization: Bearer <ADMIN_KEY>를 확인한다.
// 해시끼리 상수 시간으로 비교해 길이도 드러내지 않고, IP별 실패 5회 후에는 12초에 1회만 다시 시도할 수 있다.
type AdminAuth struct {
	keyHash  [32]byte
	ip       ClientIP
	failures *Keyed
}

func NewAdminAuth(key string, ip ClientIP) *AdminAuth {
	return &AdminAuth{keyHash: sha256.Sum256([]byte(key)), ip: ip, failures: NewKeyed(12*time.Second, 5)}
}

func (a *AdminAuth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := a.ip.From(r).String()
		if a.failures.Exhausted(ip) {
			WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(sum[:], a.keyHash[:]) != 1 {
			a.failures.Allow(ip)
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
