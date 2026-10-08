package adminapi

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const maxAllowFrom = 64

// parseAllowFrom은 /update를 부를 수 있는 IP·CIDR 목록이다. 비어 있으면 어디서든 부를 수 있다.
func parseAllowFrom(ss []string) ([]netip.Prefix, bool) {
	if len(ss) > maxAllowFrom {
		return nil, false
	}
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netutil.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

// joinPrefixes는 감사 기록용으로 정규화된 접두사를 쉼표로 잇는다.
func joinPrefixes(ps []netip.Prefix) string {
	ss := make([]string, len(ps))
	for i, p := range ps {
		ss[i] = p.String()
	}
	return strings.Join(ss, ",")
}

// domainFor는 경로의 도메인이다. 없으면 404로 답하고 ok=false.
func (a *API) domainFor(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return store.Domain{}, false
	}
	d, err := a.Store.GetDomain(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return store.Domain{}, false
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return store.Domain{}, false
	}
	return d, true
}

// activeACME는 도메인과 그 활성 ACME 계정이다. ACME가 꺼져 있으면 409 acme_disabled로 답한다.
func (a *API) activeACME(w http.ResponseWriter, r *http.Request) (store.Domain, store.ACMEState, bool) {
	d, ok := a.domainFor(w, r)
	if !ok {
		return d, store.ACMEState{}, false
	}
	st, err := a.Store.ACMEStateByDomain(r.Context(), d.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return d, st, false
	}
	if err != nil {
		a.dbError(w, "acme state", err)
		return d, st, false
	}
	return d, st, true
}

// writeACME는 갱신된 ACME 상태(도메인 상세의 acme와 같은 모양)를 응답한다.
func (a *API) writeACME(w http.ResponseWriter, r *http.Request, d store.Domain) {
	st, err := a.Store.ACMEStateByDomain(r.Context(), d.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.dbError(w, "acme state", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.acmeView(d.FQDN, &st))
}

type credentials struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Subdomain   string   `json:"subdomain"`
	FullDomain  string   `json:"fulldomain"`
	CNAMERecord string   `json:"cname_record"`
	AllowFrom   []string `json:"allowfrom"`
	// Setup은 acme.sh 설치와 웹서버별 세팅 스크립트다. 비밀값은 들어 있지 않고, 실행할 때 위 값을 입력한다.
	Setup setupInfo `json:"setup"`
	// Rotated이면 키를 재발급한 응답이다. 서버에서 세팅 스크립트를 FORCE=1로 다시 실행해야 새 키가 저장된다.
	Rotated bool `json:"rotated"`
}

// writeCredentials는 일회성 자격 증명이다. 비밀번호는 이 응답에만 나오므로 캐시하지 않게 한다.
func (a *API) writeCredentials(w http.ResponseWriter, r *http.Request, status int, fqdn string, reg acmedns.Registration, rotated bool) {
	allow := make([]string, 0, len(reg.AllowFrom))
	for _, p := range reg.AllowFrom {
		allow = append(allow, p.String())
	}
	base := a.PublicURLFor(r)
	if base == "" {
		base = a.Lang.UnknownBaseURL()
	}
	setup := buildSetup(base, "/etc/ssl/"+fqdn, a.Lang)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, status, credentials{
		Username: reg.Username, Password: reg.Password, Subdomain: reg.Subdomain, FullDomain: reg.FullDomain,
		CNAMERecord: cnameRecord(fqdn, reg.FullDomain), AllowFrom: allow, Setup: setup, Rotated: rotated,
	})
}

type allowFromRequest struct {
	AllowFrom *[]string `json:"allowfrom"`
}

func (a *API) enableACME(w http.ResponseWriter, r *http.Request) {
	d, ok := a.domainFor(w, r)
	if !ok {
		return
	}
	var req allowFromRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	var raw []string
	if req.AllowFrom != nil {
		raw = *req.AllowFrom
	}
	allow, valid := parseAllowFrom(raw)
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	reg, err := a.Svc.Register(r.Context(), d.FQDN, allow)
	switch {
	case errors.Is(err, store.ErrActiveAccountExists):
		httpx.WriteError(w, http.StatusConflict, "acme_enabled")
		return
	case errors.Is(err, acmedns.ErrInvalidDomain):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	case err != nil:
		a.serviceError(w, "enable acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_enable", Target: d.FQDN, Detail: "subdomain=" + reg.Subdomain + " allowfrom=" + joinPrefixes(allow)})
	a.writeCredentials(w, r, http.StatusCreated, d.FQDN, reg, false)
}

func (a *API) patchACME(w http.ResponseWriter, r *http.Request) {
	var req allowFromRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	if req.AllowFrom == nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	allow, valid := parseAllowFrom(*req.AllowFrom)
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	if err := a.Store.SetAllowFrom(r.Context(), st.AccountID, allow); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	} else if err != nil {
		a.dbError(w, "set allowfrom", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_update", Target: d.FQDN, Detail: "allowfrom=" + joinPrefixes(allow)})
	a.writeACME(w, r, d)
}

func (a *API) rotateACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	reg, err := a.Svc.Rotate(r.Context(), st.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.serviceError(w, "rotate acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_rotate", Target: d.FQDN})
	a.writeCredentials(w, r, http.StatusOK, d.FQDN, reg, true)
}

func (a *API) checkACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	res := a.CNAME.Check(r.Context(), d.FQDN, st.Subdomain+"."+a.Zone)
	if res.Err != nil {
		a.Log.Warn("cname check", "domain", d.FQDN, "err", res.Err)
	}
	if err := a.Store.SetCNAMEStatus(r.Context(), st.AccountID, res.Status, res.Value, a.now()); err != nil {
		a.dbError(w, "save cname status", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_check", Target: d.FQDN, Detail: "cname=" + string(res.Status)})
	a.writeACME(w, r, d)
}

func (a *API) disableACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	err := a.Svc.Disable(r.Context(), st.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.serviceError(w, "disable acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_disable", Target: d.FQDN, Detail: "subdomain=" + st.Subdomain})
	w.WriteHeader(http.StatusNoContent)
}
