package adminapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

type domainRequest struct {
	FQDN string `json:"fqdn"`
	Memo string `json:"memo"`
}

func (a *API) createDomain(w http.ResponseWriter, r *http.Request) {
	var req domainRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	fqdn := netutil.NormalizeName(req.FQDN)
	if !acmedns.ValidDomain(fqdn) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	}
	if !validMemo(req.Memo) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_memo")
		return
	}
	d, err := a.Store.CreateDomain(r.Context(), fqdn, req.Memo)
	if errors.Is(err, store.ErrDomainExists) {
		httpx.WriteError(w, http.StatusConflict, "domain_exists")
		return
	}
	if err != nil {
		a.dbError(w, "create domain", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "domain_create", Target: fqdn})
	a.writeDetail(w, r, http.StatusCreated, d.ID)
}

func (a *API) patchDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req struct {
		Memo *string `json:"memo"`
	}
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	if req.Memo != nil {
		if !validMemo(*req.Memo) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_memo")
			return
		}
		d, err := a.Store.UpdateDomainMemo(r.Context(), id, *req.Memo)
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			a.dbError(w, "update domain", err)
			return
		}
		a.audit(r, store.AuditEntry{Action: "domain_update", Target: d.FQDN})
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

// deleteDomain은 ACME가 켜져 있으면 먼저 꺼서 공급자의 챌린지 TXT를 지운 뒤 도메인을 지운다.
// 공급자 삭제가 실패하면 아무것도 지우지 않는다.
func (a *API) deleteDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	d, err := a.Store.GetDomain(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return
	}
	st, err := a.Store.ACMEStateByDomain(ctx, id)
	var detail string
	switch {
	case err == nil:
		err := a.Svc.Disable(ctx, st.AccountID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			a.serviceError(w, "delete domain: disable acme", err)
			return
		}
		detail = "subdomain=" + st.Subdomain
		if err == nil {
			// DeleteDomain이 실패해도 ACME를 껐다는 기록은 남는다.
			a.audit(r, store.AuditEntry{Action: "acme_disable", Target: d.FQDN, Detail: detail})
		}
	case !errors.Is(err, store.ErrNotFound):
		a.dbError(w, "delete domain: acme state", err)
		return
	}
	if err := a.Store.DeleteDomain(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.dbError(w, "delete domain", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "domain_delete", Target: d.FQDN, Detail: detail})
	w.WriteHeader(http.StatusNoContent)
}

func targetName(f store.TargetFields) string {
	return net.JoinHostPort(f.Hostname, strconv.Itoa(f.Port))
}

func fieldsOf(t store.Target) store.TargetFields {
	return store.TargetFields{Hostname: t.Hostname, Port: t.Port, SNI: t.SNI, IPs: t.IPs, Memo: t.Memo}
}

// checkNow는 대상을 바로 점검하고 저장한다. 저장에 실패해도 이번 결과는 돌려준다(대상 추가·수정 응답용).
func (a *API) checkNow(r *http.Request, t store.Target) []monitor.Result {
	rs, err := a.Runner.Check(r.Context(), t.Target)
	if err != nil {
		a.Log.Error("check target", "target", t.ID, "err", err)
	}
	return rs
}

func (a *API) createTarget(w http.ResponseWriter, r *http.Request) {
	domainID, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req targetRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	f, valid := req.apply(store.TargetFields{Port: 443})
	if req.Hostname == nil || !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	t, err := a.Store.CreateTarget(r.Context(), store.NewTarget{
		DomainID: domainID, Hostname: f.Hostname, Port: f.Port, SNI: f.SNI, IPs: f.IPs, Memo: f.Memo,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrTargetExists):
		httpx.WriteError(w, http.StatusConflict, "target_exists")
		return
	case err != nil:
		a.dbError(w, "create target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_create", Target: targetName(f), Detail: fmt.Sprintf("domain_id=%d", domainID)})
	httpx.WriteJSON(w, http.StatusCreated, singleTargetView(t, a.checkNow(r, t)))
}

// patchTarget은 보낸 필드만 바꾼다. 접속 설정(hostname·port·sni·ips)이 바뀌면 그 자리에서 다시 점검한다.
func (a *API) patchTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req targetRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	ctx := r.Context()
	old, err := a.Store.GetTarget(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	f, valid := req.apply(fieldsOf(old))
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	t, err := a.Store.UpdateTarget(ctx, id, f)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrTargetExists):
		httpx.WriteError(w, http.StatusConflict, "target_exists")
		return
	case err != nil:
		a.dbError(w, "update target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_update", Target: targetName(f)})
	var rs []monitor.Result
	if connectionChanged(fieldsOf(old), f) {
		rs = a.checkNow(r, t)
	} else {
		latest, err := a.Store.LatestResults(ctx, []store.Target{t})
		if err != nil {
			a.dbError(w, "target results", err)
			return
		}
		rs = latest[t.ID]
	}
	httpx.WriteJSON(w, http.StatusOK, singleTargetView(t, rs))
}

func (a *API) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	t, err := a.Store.GetTarget(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	if err := a.Store.DeleteTarget(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.dbError(w, "delete target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_delete", Target: targetName(fieldsOf(t))})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) checkTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	t, err := a.Store.GetTarget(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	rs, err := a.Runner.Check(r.Context(), t.Target)
	if err != nil {
		a.dbError(w, "check target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_check", Target: targetName(fieldsOf(t))})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": rs})
}
