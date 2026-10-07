package adminapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/session"
	"ssl-checker/internal/store"
)

const maxBodyBytes = 64 << 10

// Auth는 관리자 확인이다. 세션 쿠키나 Authorization: Bearer <ADMIN_KEY> 중 하나가 맞으면 관리자다.
// 틀린 키(로그인·Bearer)는 IP별로 5번까지 받고, 그 뒤로는 12초에 1번만 받는다.
type Auth struct {
	Sessions *session.Signer
	keyHash  [32]byte
	failures *httpx.Keyed
}

func NewAuth(adminKey string) *Auth {
	return NewAuthShared(adminKey, httpx.NewKeyed(12*time.Second, 5))
}

// NewAuthShared는 실패 한도를 다른 곳(/register의 httpx.AdminAuth)과 함께 쓴다. 같은 키를 두 곳에서 5번씩 시도할 수 없게 한다.
func NewAuthShared(adminKey string, failures *httpx.Keyed) *Auth {
	return &Auth{
		Sessions: session.New(adminKey),
		keyHash:  sha256.Sum256([]byte(adminKey)),
		failures: failures,
	}
}

// keyMatches는 해시끼리 상수 시간으로 비교해 길이도 드러내지 않는다.
func (a *Auth) keyMatches(key string) bool {
	sum := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(sum[:], a.keyHash[:]) == 1
}

// check는 요청이 관리자인지 본다. wrongKey는 Bearer 키를 보냈는데 틀린 경우다(실패로 센다).
func (a *Auth) check(r *http.Request) (admin, wrongKey bool) {
	if c, err := r.Cookie(session.CookieName); err == nil && a.Sessions.Valid(c.Value) {
		return true, false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false, false
	}
	if a.keyMatches(token) {
		return true, false
	}
	return false, true
}

// admin은 관리자만 통과시킨다.
func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 쿠키는 서명돼 있어 시도 횟수를 셀 필요가 없다. 잠긴 IP에서도 로그인한 관리자는 통과한다.
		if c, err := r.Cookie(session.CookieName); err == nil && a.Auth.Sessions.Valid(c.Value) {
			next(w, r)
			return
		}
		ip := a.IP.From(r).String()
		if a.Auth.failures.Exhausted(ip) {
			httpx.WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		ok, wrong := a.Auth.check(r)
		if wrong {
			a.Auth.failures.Allow(ip)
			a.Log.Warn("admin auth failed", "ip", ip) // 보낸 키는 남기지 않는다
		}
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// jsonOnly는 Content-Type: application/json인 요청만 받는다. 브라우저 폼은 이 타입을 보낼 수 없으므로
// 다른 사이트가 관리자의 쿠키로 쓰기 요청을 만들 수 없다. 본문은 maxBodyBytes까지만 읽는다.
func jsonOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next(w, r)
	}
}

// write는 쓰기 경로용이다: 관리자 확인 후 JSON 요구.
func (a *API) write(next http.HandlerFunc) http.HandlerFunc { return a.admin(jsonOnly(next)) }

// decode는 본문을 엄격하게 읽는다(모르는 필드 거부). 본문이 비어 있으면 v를 그대로 둔다.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// audit는 관리자 행동을 남긴다. Actor가 비면 admin이다. 클라이언트가 끊어도 기록되게 취소를 떼어 낸 ctx로 쓴다.
func (a *API) audit(r *http.Request, e store.AuditEntry) {
	if e.Actor == "" {
		e.Actor = "admin"
	}
	e.SourceIP = a.IP.From(r).String()
	if err := a.Store.Audit(context.WithoutCancel(r.Context()), e); err != nil {
		a.Log.Error("audit", "action", e.Action, "err", err)
	}
}
