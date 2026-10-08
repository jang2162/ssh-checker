package acmedns_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/store"
)

const adminKey = "test-admin-key-0123456789abcdef0123"

type fixture struct {
	t    *testing.T
	h    http.Handler
	mem  *memory.Provider
	pool *pgxpool.Pool
}

type fixtureOpts struct {
	trusted       []netip.Prefix
	burst         int
	authFailBurst int
}

func newFixture(t *testing.T, o fixtureOpts) *fixture {
	t.Helper()
	if o.burst == 0 {
		o.burst = 100
	}
	if o.authFailBurst == 0 {
		o.authFailBurst = 100
	}
	pool := dbtest.New(t)
	mem := memory.New()
	ip := httpx.ClientIP{Trusted: o.trusted}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	admin := httpx.NewAdminAuth(adminKey, ip)
	admin.Log = log
	h := &acmedns.Handler{
		Svc:           &acmedns.Service{Store: store.New(pool), Provider: mem, Zone: testZone, TTL: 60},
		IP:            ip,
		Admin:         admin,
		UpdateLimit:   httpx.NewKeyed(time.Hour, o.burst),
		AuthFailLimit: httpx.NewKeyed(time.Hour, o.authFailBurst),
		Health:        &acmedns.HealthChecker{DB: pool, Provider: mem},
		Log:           log,
	}
	return &fixture{t: t, h: h.Routes(), mem: mem, pool: pool}
}

type req struct {
	method, path, body, remote string
	headers                    map[string]string
}

func (f *fixture) do(r req) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.serve(r.build(strings.NewReader(r.body)))
}

func (r req) build(body io.Reader) *http.Request {
	hr := httptest.NewRequest(r.method, r.path, body)
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	return hr
}

func (f *fixture) serve(hr *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, hr)
	return rec
}

// hookReader는 본문이 처음 읽힐 때 fn을 한 번 부른다. 핸들러는 인증한 뒤에 본문을 읽으므로
// "인증은 끝났고 계정 행은 아직 잠그지 않은" 시점에 끼어들 수 있다.
type hookReader struct {
	r    io.Reader
	fn   func()
	once sync.Once
}

func (h *hookReader) Read(p []byte) (int, error) {
	h.once.Do(h.fn)
	return h.r.Read(p)
}

// auditActors는 action·detail이 같은 감사 로그 행의 actor를 돌려준다.
func (f *fixture) auditActors(action, detail string) []string {
	f.t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT actor FROM audit_logs WHERE action = $1 AND detail = $2 ORDER BY id`, action, detail)
	if err != nil {
		f.t.Fatal(err)
	}
	actors, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		f.t.Fatal(err)
	}
	return actors
}

type registration struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullDomain string   `json:"fulldomain"`
	Subdomain  string   `json:"subdomain"`
	AllowFrom  []string `json:"allowfrom"`
}

func registerReq(body string) req {
	return req{method: "POST", path: "/register", body: body, headers: map[string]string{"Authorization": "Bearer " + adminKey}}
}

func (f *fixture) register(domain string, allow ...string) registration {
	f.t.Helper()
	body, _ := json.Marshal(map[string]any{"domain": domain, "allowfrom": allow})
	rec := f.do(registerReq(string(body)))
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var reg registration
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		f.t.Fatal(err)
	}
	return reg
}

func updateReq(reg registration, value string) req {
	body, _ := json.Marshal(map[string]string{"subdomain": reg.Subdomain, "txt": value})
	return req{method: "POST", path: "/update", body: string(body),
		headers: map[string]string{"X-Api-User": reg.Username, "X-Api-Key": reg.Password}}
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, body map[string]string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	if !maps.Equal(got, body) {
		t.Fatalf("body = %v, want %v", got, body)
	}
}

func (f *fixture) txtCount() int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM txt_records`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestRegisterEndpoint(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	body := `{"domain":"a.com","allowfrom":["10.1.2.3/16","192.168.0.5"]}`
	rec := f.do(registerReq(body))
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	var reg registration
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.FullDomain != reg.Subdomain+"."+testZone || len(reg.Password) != 40 {
		t.Fatalf("reg = %+v", reg)
	}
	if strings.Join(reg.AllowFrom, ",") != "10.1.0.0/16,192.168.0.5/32" {
		t.Fatalf("allowfrom = %v", reg.AllowFrom)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'register' AND target = 'a.com'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows = %d, %v", n, err)
	}
}

func TestRegisterRequiresAdminKey(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	for _, h := range []map[string]string{nil, {"Authorization": "Bearer wrong"}} {
		rec := f.do(req{method: "POST", path: "/register", body: `{"domain":"a.com"}`, headers: h})
		expect(t, rec, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	expect(t, f.do(registerReq(`{not json`)), http.StatusBadRequest, map[string]string{"error": "malformed_json_payload"})
	expect(t, f.do(registerReq(`{"domain":"a.com","allowfrom":["10.0.0.0/99"]}`)), http.StatusBadRequest, map[string]string{"error": "invalid_allowfrom_cidr"})
	expect(t, f.do(registerReq(`{"domain":"*.a.com"}`)), http.StatusBadRequest, map[string]string{"error": "invalid_domain"})
	f.register("a.com")
	expect(t, f.do(registerReq(`{"domain":"a.com"}`)), http.StatusConflict, map[string]string{"error": "account_exists"})
}

func TestUpdateStoresAndRotates(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	for _, v := range []string{txt('a'), txt('b'), txt('c')} {
		expect(t, f.do(updateReq(reg, v)), http.StatusOK, map[string]string{"txt": v})
	}
	got := f.mem.Values(reg.FullDomain)
	if strings.Join(got, ",") != txt('b')+","+txt('c') {
		t.Fatalf("provider = %v", got)
	}
}

func TestUpdateForbidden(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	other := f.register("b.com")
	forbidden := map[string]string{"error": "forbidden"}
	mutate := func(fn func(*req)) req { r := updateReq(reg, txt('a')); fn(&r); return r }
	cases := map[string]req{
		"wrong key":       mutate(func(r *req) { r.headers["X-Api-Key"] = "wrong" }),
		"long key":        mutate(func(r *req) { r.headers["X-Api-Key"] = strings.Repeat("k", 100) }),
		"unknown user":    mutate(func(r *req) { r.headers["X-Api-User"] = "11111111-1111-1111-1111-111111111111" }),
		"non-uuid user":   mutate(func(r *req) { r.headers["X-Api-User"] = "abc" }),
		"missing headers": mutate(func(r *req) { r.headers = nil }),
		"other subdomain": mutate(func(r *req) { r.body = `{"subdomain":"` + other.Subdomain + `","txt":"` + txt('a') + `"}` }),
		"malformed json":  mutate(func(r *req) { r.body = `{not json` }),
		"empty body":      mutate(func(r *req) { r.body = `` }),
		"oversized body": mutate(func(r *req) {
			r.body = `{"subdomain":"` + reg.Subdomain + `","txt":"` + strings.Repeat("a", 5000) + `"}`
		}),
		"other account keys": mutate(func(r *req) { r.headers["X-Api-User"] = other.Username; r.headers["X-Api-Key"] = other.Password }),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) { expect(t, f.do(r), http.StatusUnauthorized, forbidden) })
	}
	if f.txtCount() != 0 || len(f.mem.Values(reg.FullDomain)) != 0 {
		t.Fatal("forbidden request wrote values")
	}
}

func TestUpdateBadTXT(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	for _, v := range []string{"", strings.Repeat("A", 42), strings.Repeat("+", 43)} {
		expect(t, f.do(updateReq(reg, v)), http.StatusBadRequest, map[string]string{"error": "bad_txt"})
	}
}

func TestUpdateAllowFrom(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com", "10.1.0.0/16")
	ok := updateReq(reg, txt('a'))
	ok.remote = "10.1.2.3:4000"
	expect(t, f.do(ok), http.StatusOK, map[string]string{"txt": txt('a')})
	denied := updateReq(reg, txt('b'))
	denied.remote = "10.2.0.1:4000"
	expect(t, f.do(denied), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
}

func TestUpdateBehindTrustedProxy(t *testing.T) {
	f := newFixture(t, fixtureOpts{trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	reg := f.register("a.com", "192.168.5.0/24")
	cases := []struct {
		name, remote, xff string
		status            int
	}{
		{"proxy forwards allowed client", "10.0.0.2:4000", "192.168.5.7", http.StatusOK},
		{"untrusted client spoofs header", "203.0.113.9:4000", "192.168.5.7", http.StatusUnauthorized},
		{"spoofed leftmost hop ignored", "10.0.0.2:4000", "192.168.5.7, 203.0.113.50", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := updateReq(reg, txt('a'))
			r.remote = c.remote
			r.headers["X-Forwarded-For"] = c.xff
			if rec := f.do(r); rec.Code != c.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.status, rec.Body)
			}
		})
	}
}

func TestUpdateProviderFailure(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	f.mem.SetFailure(errors.New("cloudflare down"))
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusInternalServerError, map[string]string{"error": "dns_provider_error"})
	if f.txtCount() != 0 {
		t.Fatal("db written despite provider failure")
	}
	var detail string
	if err := f.pool.QueryRow(context.Background(), `SELECT detail FROM audit_logs WHERE action = 'update_failed'`).Scan(&detail); err != nil || !strings.HasPrefix(detail, "transient") {
		t.Fatalf("audit detail = %q, %v", detail, err)
	}
	f.mem.SetFailure(nil)
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
}

func TestUpdateRevokedAccount(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	if _, err := f.pool.Exec(context.Background(), `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
}

func TestUpdateRateLimit(t *testing.T) {
	f := newFixture(t, fixtureOpts{burst: 2})
	reg := f.register("a.com")
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
	expect(t, f.do(updateReq(reg, txt('b'))), http.StatusOK, map[string]string{"txt": txt('b')})
	expect(t, f.do(updateReq(reg, txt('c'))), http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
}

func TestHealth(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	expect(t, f.do(req{method: "GET", path: "/health"}), http.StatusOK, map[string]string{"db": "ok", "provider": "ok"})
	f.mem.SetFailure(errors.New("down"))
	expect(t, f.do(req{method: "GET", path: "/health"}), http.StatusServiceUnavailable, map[string]string{"db": "ok", "provider": "transient"})
}

func TestUpdateDeniedAuditsInvalidUTF8User(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	r := updateReq(reg, txt('a'))
	r.headers["X-Api-User"] = "\xff\xfe" + strings.Repeat("\u00e9", 40)
	expect(t, f.do(r), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'update_denied'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("update_denied rows = %d, %v", n, err)
	}
}

// ctx를 존중하는 공급자: 취소된 ctx로 부르면 에러를 돌려준다.
type ctxProvider struct{ dnsprovider.Provider }

func (ctxProvider) Check(ctx context.Context) error { return ctx.Err() }

func TestHealthCacheIgnoresCallerCancellation(t *testing.T) {
	h := &acmedns.HealthChecker{DB: pingOK{}, Provider: ctxProvider{}, CacheFor: time.Hour}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	h.Check(cancelled)
	st, healthy := h.Check(context.Background())
	if !healthy || st.Provider != "ok" {
		t.Fatalf("cancelled caller poisoned cache: %+v healthy=%v", st, healthy)
	}
}

type pingOK struct{}

func (pingOK) Ping(context.Context) error { return nil }

func TestUpdateAuthFailureLimitPerIP(t *testing.T) {
	f := newFixture(t, fixtureOpts{authFailBurst: 3})
	reg := f.register("a.com")
	fenced := f.register("b.com", "10.9.0.0/16")
	forbidden := map[string]string{"error": "forbidden"}
	tooMany := map[string]string{"error": "too_many_attempts"}
	from := func(r req, remote string) req { r.remote = remote; return r }
	const attacker = "198.51.100.1:4000"

	// 거부 사유 세 가지가 모두 같은 IP 예산을 쓴다.
	wrongKey := updateReq(reg, txt('a'))
	wrongKey.headers["X-Api-Key"] = "wrong"
	mismatch := updateReq(reg, txt('a'))
	mismatch.body = `{"subdomain":"` + fenced.Subdomain + `","txt":"` + txt('a') + `"}`
	for _, r := range []req{wrongKey, mismatch, updateReq(fenced, txt('a'))} {
		expect(t, f.do(from(r, attacker)), http.StatusUnauthorized, forbidden)
	}
	expect(t, f.do(from(wrongKey, attacker)), http.StatusTooManyRequests, tooMany)
	// 막힌 IP는 올바른 키를 보내도 인증 전에 끊긴다.
	expect(t, f.do(from(updateReq(reg, txt('a')), attacker)), http.StatusTooManyRequests, tooMany)

	expect(t, f.do(from(wrongKey, "198.51.100.2:4000")), http.StatusUnauthorized, forbidden)
	expect(t, f.do(from(updateReq(reg, txt('b')), "198.51.100.3:4000")), http.StatusOK, map[string]string{"txt": txt('b')})

	// 429는 감사 로그를 남기지 않는다: 거부 4건(공격 IP 3건, 다른 IP 1건)만 남는다.
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'update_denied'`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("update_denied rows = %d, %v", n, err)
	}
}

// 인증과 행 잠금 사이에 계정이 폐기되면 500이 아니라 인증 실패와 같은 401이다.
func TestUpdateAccountRevokedAfterAuthenticate(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	r := updateReq(reg, txt('a'))
	revoke := func() {
		if _, err := f.pool.Exec(context.Background(), `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
			t.Error(err)
		}
	}
	rec := f.serve(r.build(&hookReader{r: strings.NewReader(r.body), fn: revoke}))
	expect(t, rec, http.StatusUnauthorized, map[string]string{"error": "forbidden"})
	if got := f.auditActors("update_denied", "invalid_credentials"); !slices.Equal(got, []string{"account:" + reg.Username}) {
		t.Fatalf("update_denied actors = %v", got)
	}
	if n := len(f.auditActors("update_failed", "")); n != 0 || f.txtCount() != 0 || len(f.mem.Values(reg.FullDomain)) != 0 {
		t.Fatal("revoked account was written or audited as failure")
	}
}

func TestUpdateDeniedAuditsBadTXTAndRateLimit(t *testing.T) {
	f := newFixture(t, fixtureOpts{burst: 2})
	reg := f.register("a.com")
	actor := "account:" + reg.Username
	expect(t, f.do(updateReq(reg, "short")), http.StatusBadRequest, map[string]string{"error": "bad_txt"})
	if got := f.auditActors("update_denied", "bad_txt"); !slices.Equal(got, []string{actor}) {
		t.Fatalf("bad_txt actors = %v", got)
	}
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
	expect(t, f.do(updateReq(reg, txt('b'))), http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
	if got := f.auditActors("update_denied", "rate_limited"); !slices.Equal(got, []string{actor}) {
		t.Fatalf("rate_limited actors = %v", got)
	}
}

// 클라이언트가 연결을 끊어 요청 ctx가 취소돼도 감사 로그는 남아야 한다.
func TestUpdateAuditSurvivesClientDisconnect(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	other := f.register("b.com")
	r := updateReq(reg, txt('a'))
	r.body = `{"subdomain":"` + other.Subdomain + `","txt":"` + txt('a') + `"}`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := f.serve(r.build(&hookReader{r: strings.NewReader(r.body), fn: cancel}).WithContext(ctx))
	expect(t, rec, http.StatusUnauthorized, map[string]string{"error": "forbidden"})
	if got := f.auditActors("update_denied", "subdomain_mismatch"); !slices.Equal(got, []string{"account:" + reg.Username}) {
		t.Fatalf("subdomain_mismatch actors = %v", got)
	}
}

func (f *fixture) acmeState(domain string) store.ACMEState {
	f.t.Helper()
	states, err := store.New(f.pool).ActiveACMEStates(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, s := range states {
		if s.Domain == domain {
			return s
		}
	}
	f.t.Fatalf("no acme state for %s", domain)
	return store.ACMEState{}
}

func TestUpdateRecordsACMEState(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
	if s := f.acmeState("a.com"); s.LastUpdateAt == nil || s.ConsecutiveFailures != 0 || s.LastFailureAt != nil {
		t.Fatalf("after success = %+v", s)
	}

	f.mem.SetFailure(errors.New("cloudflare down"))
	for range 3 {
		expect(t, f.do(updateReq(reg, txt('b'))), http.StatusInternalServerError, map[string]string{"error": "dns_provider_error"})
	}
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 3 || s.LastFailureAt == nil {
		t.Fatalf("after failures = %+v", s)
	}

	f.mem.SetFailure(nil)
	expect(t, f.do(updateReq(reg, txt('b'))), http.StatusOK, map[string]string{"txt": txt('b')})
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 0 {
		t.Fatalf("after recovery = %+v", s)
	}
}

func TestUpdateRejectedValueIsNotAFailure(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	expect(t, f.do(updateReq(reg, "short")), http.StatusBadRequest, map[string]string{"error": "bad_txt"})
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 0 || s.LastFailureAt != nil {
		t.Fatalf("bad_txt counted as failure: %+v", s)
	}
}

func verifyReq(reg registration, remote string) req {
	return req{method: "POST", path: "/verify", remote: remote,
		headers: map[string]string{"X-Api-User": reg.Username, "X-Api-Key": reg.Password}}
}

// /verify는 TXT를 건드리지 않고, 맞는 자격 증명에는 호출한 IP를, 허용 IP 밖이면 그 IP와 함께 알려 준다.
func TestVerify(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com", "10.1.0.0/16")

	rec := f.do(verifyReq(reg, "10.1.2.3:4000"))
	var ok map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &ok) != nil || ok["ok"] != true || ok["source_ip"] != "10.1.2.3" {
		t.Fatalf("허용 IP = %d %s", rec.Code, rec.Body)
	}
	if f.txtCount() != 0 {
		t.Error("/verify가 TXT를 기록했다")
	}

	// 자격 증명이 맞고 IP만 허용 밖이면, 호출한 IP를 알려 준다.
	expect(t, f.do(verifyReq(reg, "203.0.113.9:4000")), http.StatusForbidden, map[string]string{"error": "ip_not_allowed", "source_ip": "203.0.113.9"})

	// 틀린 키는 /update처럼 401 forbidden이고, 허용 IP 여부는 알려 주지 않는다.
	bad := verifyReq(reg, "203.0.113.9:4000")
	bad.headers["X-Api-Key"] = "wrong"
	expect(t, f.do(bad), http.StatusUnauthorized, map[string]string{"error": "forbidden"})

	if got := f.auditActors("verify_denied", "ip_unauthorized"); len(got) != 1 {
		t.Errorf("verify_denied(ip_unauthorized) = %v", got)
	}
}

// /verify 거부도 /update와 같은 IP별 실패 예산을 써서, 막힌 IP는 올바른 키도 429로 끊긴다.
func TestVerifySharesAuthFailureBudget(t *testing.T) {
	f := newFixture(t, fixtureOpts{authFailBurst: 2})
	reg := f.register("a.com", "10.1.0.0/16")
	const attacker = "203.0.113.9:4000"
	expect(t, f.do(verifyReq(reg, attacker)), http.StatusForbidden, map[string]string{"error": "ip_not_allowed", "source_ip": "203.0.113.9"})
	bad := verifyReq(reg, attacker)
	bad.headers["X-Api-Key"] = "wrong"
	expect(t, f.do(bad), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
	expect(t, f.do(verifyReq(reg, attacker)), http.StatusTooManyRequests, map[string]string{"error": "too_many_attempts"})
	// /update도 같은 예산이라 막혀 있다.
	u := updateReq(reg, txt('a'))
	u.remote = attacker
	expect(t, f.do(u), http.StatusTooManyRequests, map[string]string{"error": "too_many_attempts"})
}

// /acme/ 아래 경로는 루트의 /update, /verify와 같은 동작이다. ACMEDNS_BASE_URL을 <주소>/acme로 두면 acme.sh가 이 경로를 부른다.
func TestAcmePrefixMatchesRootPaths(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com", "10.1.0.0/16")

	up := updateReq(reg, txt('a'))
	up.path = "/acme/update"
	up.remote = "10.1.2.3:4000"
	expect(t, f.do(up), http.StatusOK, map[string]string{"txt": txt('a')})

	ver := verifyReq(reg, "203.0.113.9:4000")
	ver.path = "/acme/verify"
	expect(t, f.do(ver), http.StatusForbidden, map[string]string{"error": "ip_not_allowed", "source_ip": "203.0.113.9"})
}
