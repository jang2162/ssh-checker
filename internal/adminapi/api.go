// Package adminapi는 대시보드와 에이전트가 쓰는 /api JSON API다. 이 계획에서는 조회만 연다.
package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

const (
	defaultResultLimit = 50
	maxResultLimit     = 500
)

type API struct {
	Store         *store.Store
	Zone          string        // CHALLENGE_ZONE
	CheckInterval time.Duration // 화면에 보여 주기만 한다. 점검은 worker가 한다
	PublicURL     string        // 비우면 요청에서 만든다
	IP            httpx.ClientIP
	Now           func() time.Time
	Log           *slog.Logger
}

// Register는 조회 경로를 mux에 붙인다. 조회는 인증 없이 누구나 할 수 있다.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/summary", a.summary)
	mux.HandleFunc("GET /api/domains", a.listDomains)
	mux.HandleFunc("GET /api/domains/{id}", a.getDomain)
	mux.HandleFunc("GET /api/targets/{id}/results", a.targetResults)
	mux.HandleFunc("GET /api/config", a.config)
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *API) dbError(w http.ResponseWriter, op string, err error) {
	a.Log.Error(op, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "db_error")
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// allDomains는 목록·요약에 쓰는 도메인별 요약이다. 도메인 수가 적어 매번 전체를 계산한다.
func (a *API) allDomains(ctx context.Context) ([]domainSummary, error) {
	domains, err := a.Store.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := a.Store.ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		return nil, err
	}
	states, err := a.Store.ActiveACMEStates(ctx)
	if err != nil {
		return nil, err
	}
	byDomain := map[int64][]store.Target{}
	for _, t := range targets {
		byDomain[t.DomainID] = append(byDomain[t.DomainID], t)
	}
	acme := map[int64]*store.ACMEState{}
	for i := range states {
		acme[states[i].DomainID] = &states[i]
	}
	now := a.now()
	out := make([]domainSummary, 0, len(domains))
	for _, d := range domains {
		out = append(out, summarize(d, byDomain[d.ID], latest, acme[d.ID], now))
	}
	return out, nil
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	ds, err := a.allDomains(r.Context())
	if err != nil {
		a.dbError(w, "summary", err)
		return
	}
	counts := map[monitor.Status]int{}
	var checked *time.Time
	for _, d := range ds {
		counts[d.Status]++
		if d.LastCheckedAt != nil && (checked == nil || d.LastCheckedAt.After(*checked)) {
			checked = d.LastCheckedAt
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"domains": len(ds), "critical": counts[monitor.StatusCritical], "warning": counts[monitor.StatusWarning],
		"info": counts[monitor.StatusInfo], "ok": counts[monitor.StatusOK], "unknown": counts[monitor.StatusUnknown],
		"checked_at": checked,
	})
}

func (a *API) listDomains(w http.ResponseWriter, r *http.Request) {
	filter := monitor.Status(r.URL.Query().Get("status"))
	if filter != "" && !filter.Valid() {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status")
		return
	}
	ds, err := a.allDomains(r.Context())
	if err != nil {
		a.dbError(w, "list domains", err)
		return
	}
	out := []domainSummary{}
	for _, d := range ds {
		if filter == "" || d.Status == filter {
			out = append(out, d)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"domains": out})
}

func (a *API) getDomain(w http.ResponseWriter, r *http.Request) {
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
	targets, err := a.Store.TargetsByDomain(ctx, id)
	if err != nil {
		a.dbError(w, "get domain targets", err)
		return
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		a.dbError(w, "get domain results", err)
		return
	}
	var acme *store.ACMEState
	switch st, err := a.Store.ACMEStateByDomain(ctx, id); {
	case err == nil:
		acme = &st
	case !errors.Is(err, store.ErrNotFound):
		a.dbError(w, "get domain acme", err)
		return
	}
	overall, per := assess(targets, latest, acme)
	httpx.WriteJSON(w, http.StatusOK, domainDetail{
		ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, CreatedAt: d.CreatedAt, Assessment: overall,
		Targets: targetViews(targets, latest, per), ACME: a.acmeView(d.FQDN, acme),
	})
}

func (a *API) targetResults(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	limit := defaultResultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = min(n, maxResultLimit)
	}
	ctx := r.Context()
	if _, err := a.Store.GetTarget(ctx, id); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	rs, err := a.Store.ResultHistory(ctx, id, limit)
	if err != nil {
		a.dbError(w, "target results", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": rs})
}

func (a *API) config(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"challenge_zone": a.Zone,
		"check_interval": a.CheckInterval.String(),
		"public_url":     a.PublicURLFor(r),
	})
}

// PublicURLFor는 클라이언트가 api에 닿는 주소다. PUBLIC_URL이 있으면 그 값, 없으면 요청에서 만든다.
// X-Forwarded-Proto/Host는 신뢰하는 프록시에서 직접 온 요청일 때만 믿는다.
func (a *API) PublicURLFor(r *http.Request) string {
	if a.PublicURL != "" {
		return a.PublicURL
	}
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if a.IP.Proxied(r) {
		if p := firstValue(r.Header.Get("X-Forwarded-Proto")); p == "http" || p == "https" {
			scheme = p
		}
		if h := firstValue(r.Header.Get("X-Forwarded-Host")); h != "" {
			host = h
		}
	}
	return scheme + "://" + host
}

func firstValue(s string) string {
	v, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(v)
}
