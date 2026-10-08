// Package adminapi는 대시보드와 에이전트가 쓰는 /api JSON API다.
package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/dnsprovider"
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
	Auth          *Auth               // 관리자 확인
	Svc           *acmedns.Service    // ACME 켜기·재발급·끄기
	Runner        *monitor.Runner     // "지금 점검"과 대상 추가·수정 직후 점검
	CNAME         *cnamecheck.Checker // ACME CNAME "지금 확인"
}

type route struct {
	method, pattern string
	h               http.HandlerFunc
}

// routes는 /api 경로 전체다. 조회는 누구나, 쓰기와 감사 로그는 관리자만.
func (a *API) routes() []route {
	return []route{
		{"GET", "/api/summary", a.summary},
		{"GET", "/api/domains", a.listDomains},
		{"GET", "/api/domains/{id}", a.getDomain},
		{"GET", "/api/targets/{id}/results", a.targetResults},
		{"GET", "/api/config", a.config},
		{"GET", "/api/codes", a.codes},
		{"GET", "/api/setup", a.setup},
		{"GET", "/api/session", a.sessionStatus},
		{"POST", "/api/session", jsonOnly(a.login)},
		{"DELETE", "/api/session", jsonOnly(a.logout)},
		{"GET", "/api/audit", a.admin(a.listAudit)},
		{"POST", "/api/domains", a.write(a.createDomain)},
		{"PATCH", "/api/domains/{id}", a.write(a.patchDomain)},
		{"DELETE", "/api/domains/{id}", a.write(a.deleteDomain)},
		{"POST", "/api/domains/{id}/targets", a.write(a.createTarget)},
		{"PATCH", "/api/targets/{id}", a.write(a.patchTarget)},
		{"DELETE", "/api/targets/{id}", a.write(a.deleteTarget)},
		{"POST", "/api/targets/{id}/check", a.write(a.checkTarget)},
		{"POST", "/api/domains/{id}/acme", a.write(a.enableACME)},
		{"PATCH", "/api/domains/{id}/acme", a.write(a.patchACME)},
		{"DELETE", "/api/domains/{id}/acme", a.write(a.disableACME)},
		{"POST", "/api/domains/{id}/acme/rotate", a.write(a.rotateACME)},
		{"POST", "/api/domains/{id}/acme/check", a.write(a.checkACME)},
	}
}

// Register는 /api 경로를 mux에 붙인다. 모든 /api 응답은 JSON이다: 없는 경로는 404,
// 있는 경로에 받지 않는 메서드는 405(Allow 헤더 포함).
func (a *API) Register(mux *http.ServeMux) {
	allow := map[string][]string{}
	probe := http.NewServeMux() // 메서드 없이 경로만 등록해, catch-all에서 "경로는 있는데 메서드가 다름"을 가린다
	for _, rt := range a.routes() {
		mux.HandleFunc(rt.method+" "+rt.pattern, rt.h)
		if _, seen := allow[rt.pattern]; !seen {
			probe.HandleFunc(rt.pattern, func(http.ResponseWriter, *http.Request) {})
		}
		allow[rt.pattern] = append(allow[rt.pattern], rt.method)
	}
	// 인증서를 받는 서버가 curl로 받는 스크립트(acme.sh 설치 확인부터 발급·설치까지). 와일드카드 없이 두어 GET /acme/update는 405로 남는다.
	mux.HandleFunc("GET /acme/setup.sh", a.SetupScript)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := probe.Handler(r); pattern != "" {
			methods := allow[pattern]
			if slices.Contains(methods, http.MethodGet) {
				methods = append(slices.Clone(methods), http.MethodHead)
			}
			w.Header().Set("Allow", strings.Join(methods, ", "))
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		httpx.WriteError(w, http.StatusNotFound, "not_found")
	})
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

// serviceError는 ACME 작업 실패를 응답한다. DNS 공급자 실패와 DB 실패를 구분한다.
func (a *API) serviceError(w http.ResponseWriter, op string, err error) {
	a.Log.Error(op, "err", err)
	var perr *dnsprovider.Error
	if errors.As(err, &perr) {
		httpx.WriteError(w, http.StatusInternalServerError, "dns_provider_error")
		return
	}
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

// loadDetail은 도메인 상세 응답이다. 도메인이 없으면 store.ErrNotFound다.
func (a *API) loadDetail(ctx context.Context, id int64) (domainDetail, error) {
	d, err := a.Store.GetDomain(ctx, id)
	if err != nil {
		return domainDetail{}, err
	}
	targets, err := a.Store.TargetsByDomain(ctx, id)
	if err != nil {
		return domainDetail{}, err
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		return domainDetail{}, err
	}
	var acme *store.ACMEState
	switch st, err := a.Store.ACMEStateByDomain(ctx, id); {
	case err == nil:
		acme = &st
	case !errors.Is(err, store.ErrNotFound):
		return domainDetail{}, err
	}
	overall, per := assess(targets, latest, acme)
	return domainDetail{
		ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, CreatedAt: d.CreatedAt, Assessment: overall,
		Targets: targetViews(targets, latest, per), ACME: a.acmeView(d.FQDN, acme),
	}, nil
}

func (a *API) getDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

// writeDetail은 도메인 상세를 status로 응답한다.
func (a *API) writeDetail(w http.ResponseWriter, r *http.Request, status int, id int64) {
	detail, err := a.loadDetail(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return
	}
	httpx.WriteJSON(w, status, detail)
}

func (a *API) targetResults(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	limit, ok := parseLimit(r, defaultResultLimit, maxResultLimit)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
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

// codes는 판정 코드 표다. 화면과 llms.txt가 같은 설명을 쓰도록 한 곳에서 낸다.
func (a *API) codes(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"codes": monitor.Codes()})
}

// hostRE는 이름[:포트] 또는 [IPv6][:포트]다. 공개 주소는 자격 증명 안내에 들어가므로 경로·사용자 정보·공백이 섞인 값은 받지 않는다.
var hostRE = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)

// PublicURLFor는 클라이언트가 api에 닿는 주소다. PUBLIC_URL이 있으면 그 값, 없으면 요청에서 만든다.
// X-Forwarded-Proto/Host는 신뢰하는 프록시에서 직접 온 요청일 때만 믿는다. 쓸 수 있는 호스트가 없으면 빈 문자열이다.
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
		if h := firstValue(r.Header.Get("X-Forwarded-Host")); hostRE.MatchString(h) {
			host = h
		}
	}
	if !hostRE.MatchString(host) {
		return ""
	}
	return scheme + "://" + host
}

func firstValue(s string) string {
	v, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(v)
}

// parseLimit은 ?limit= 값이다. 없으면 def, max보다 크면 max로 줄인다. 1보다 작거나 숫자가 아니면 ok=false.
func parseLimit(r *http.Request, def, max int) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, false
	}
	return min(n, max), true
}
