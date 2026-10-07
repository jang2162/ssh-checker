package adminapi

import (
	"net/http"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/session"
	"ssl-checker/internal/store"
)

type loginRequest struct {
	Key string `json:"key"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	ip := a.IP.From(r).String()
	if a.Auth.failures.Exhausted(ip) {
		httpx.WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	var req loginRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	if !a.Auth.keyMatches(req.Key) {
		a.Auth.failures.Allow(ip)
		a.audit(r, store.AuditEntry{Actor: "anonymous", Action: "login_denied"})
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	value, exp := a.Auth.Sessions.Issue()
	http.SetCookie(w, a.cookie(r, value, exp.Sub(a.now())))
	a.audit(r, store.AuditEntry{Action: "login"})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"admin": true})
}

func (a *API) sessionStatus(w http.ResponseWriter, r *http.Request) {
	ok, _ := a.Auth.check(r)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"admin": ok})
}

// logout은 브라우저의 쿠키만 지운다. 서버에 세션이 없으므로 이미 복사된 쿠키는 만료까지 유효하다.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	c := a.cookie(r, "", 0)
	c.MaxAge = -1
	http.SetCookie(w, c)
	w.WriteHeader(http.StatusNoContent)
}

// cookie의 Secure는 공개 주소가 http면 뺀다. 붙이면 브라우저가 http로는 쿠키를 보내지 않아 로그인이 안 된다.
func (a *API) cookie(r *http.Request, value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name: session.CookieName, Value: value, Path: "/",
		MaxAge: int(ttl.Seconds()), HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: !strings.HasPrefix(a.PublicURLFor(r), "http://"),
	}
}
