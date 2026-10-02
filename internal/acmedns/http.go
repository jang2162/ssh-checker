package acmedns

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const (
	maxBodyBytes = 4 << 10
	maxActorLen  = 64
)

type Handler struct {
	Svc         *Service
	IP          httpx.ClientIP
	Admin       *httpx.AdminAuth
	UpdateLimit *httpx.Keyed
	Health      *HealthChecker
	Log         *slog.Logger
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /update", h.update)
	mux.Handle("POST /register", h.Admin.Wrap(http.HandlerFunc(h.register)))
	mux.HandleFunc("GET /health", h.health)
	return mux
}

type updateRequest struct {
	Subdomain string `json:"subdomain"`
	TXT       string `json:"txt"`
}

// update는 acme.sh dns_acmedns가 부르는 유일한 API다. 검사 순서와 응답은 원본 acme-dns를 따른다.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := h.IP.From(r)
	user := truncate(r.Header.Get("X-Api-User"), maxActorLen)
	deny := func(reason string) {
		h.audit(ctx, store.AuditEntry{Actor: "account:" + user, Action: "update_denied", SourceIP: ip.String(), Detail: reason})
		httpx.WriteError(w, http.StatusUnauthorized, "forbidden")
	}

	acct, err := h.Svc.Authenticate(ctx, r.Header.Get("X-Api-User"), r.Header.Get("X-Api-Key"))
	if errors.Is(err, ErrForbidden) {
		deny("invalid_credentials")
		return
	}
	if err != nil {
		h.Log.Error("authenticate", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !allowed(acct.AllowFrom, ip) {
		deny("ip_unauthorized")
		return
	}
	var req updateRequest
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req) // 해석 실패는 빈 값으로 두고 아래 일치 검사에서 거부한다
	if req.Subdomain != acct.Subdomain {
		deny("subdomain_mismatch")
		return
	}
	if !h.UpdateLimit.Allow(acct.Username) {
		httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	target := h.Svc.FQDN(acct.Subdomain)
	actor := "account:" + acct.Username
	err = h.Svc.UpdateTXT(ctx, acct, req.TXT)
	switch {
	case errors.Is(err, ErrBadTXT):
		httpx.WriteError(w, http.StatusBadRequest, "bad_txt")
	case err != nil:
		code, detail := "db_error", err.Error()
		var perr *dnsprovider.Error
		if errors.As(err, &perr) {
			code, detail = "dns_provider_error", perr.Kind.String()+": "+err.Error()
		}
		h.Log.Error("update txt", "subdomain", acct.Subdomain, "err", err)
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update_failed", Target: target, SourceIP: ip.String(), Detail: detail})
		httpx.WriteError(w, http.StatusInternalServerError, code)
	default:
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update", Target: target, SourceIP: ip.String()})
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"txt": req.TXT})
	}
}

type registerRequest struct {
	Domain    string   `json:"domain"`
	AllowFrom []string `json:"allowfrom"`
}

type registerResponse struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullDomain string   `json:"fulldomain"`
	Subdomain  string   `json:"subdomain"`
	AllowFrom  []string `json:"allowfrom"`
}

// register는 관리자 전용이다. 응답의 password는 이때 한 번만 나온다.
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json_payload")
		return
	}
	prefixes := make([]netip.Prefix, 0, len(req.AllowFrom))
	for _, s := range req.AllowFrom {
		p, err := netutil.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom_cidr")
			return
		}
		prefixes = append(prefixes, p)
	}
	reg, err := h.Svc.Register(ctx, req.Domain, prefixes)
	switch {
	case errors.Is(err, ErrInvalidDomain):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	case errors.Is(err, store.ErrActiveAccountExists):
		httpx.WriteError(w, http.StatusConflict, "account_exists")
		return
	case err != nil:
		h.Log.Error("register", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "db_error")
		return
	}
	h.audit(ctx, store.AuditEntry{Actor: "admin", Action: "register", Target: netutil.NormalizeName(req.Domain),
		SourceIP: h.IP.From(r).String(), Detail: "subdomain=" + reg.Subdomain})

	allow := make([]string, 0, len(reg.AllowFrom))
	for _, p := range reg.AllowFrom {
		allow = append(allow, p.String())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, registerResponse{
		Username: reg.Username, Password: reg.Password, FullDomain: reg.FullDomain, Subdomain: reg.Subdomain, AllowFrom: allow,
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	st, healthy := h.Health.Check(r.Context())
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, st)
}

func (h *Handler) audit(ctx context.Context, e store.AuditEntry) {
	if err := h.Svc.Store.Audit(ctx, e); err != nil {
		h.Log.Error("audit", "action", e.Action, "err", err)
	}
}

func allowed(prefixes []netip.Prefix, ip netip.Addr) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
