package adminapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/adminapi"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

const adminKey = "test-admin-key-0123456789abcdef0123"

type fixture struct {
	t   *testing.T
	st  *store.Store
	api *adminapi.API
	h   http.Handler
	mem *memory.Provider
	svc *acmedns.Service
}

func newFixture(t *testing.T) *fixture {
	pool := dbtest.New(t)
	st := store.New(pool)
	mem := memory.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &acmedns.Service{Store: st, Provider: mem, Zone: "acme.test", TTL: 60}
	api := &adminapi.API{
		Store: st, Svc: svc, Zone: "acme.test", CheckInterval: 6 * time.Hour,
		Runner: &monitor.Runner{Prober: &monitor.Prober{Timeout: 300 * time.Millisecond}, Sink: st, Log: log},
		Auth:   adminapi.NewAuth(adminKey),
		CNAME:  &cnamecheck.Checker{Resolver: "127.0.0.1:1", Timeout: 200 * time.Millisecond},
		IP:     httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		Now:    func() time.Time { return now }, Log: log,
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &fixture{t: t, st: st, api: api, h: mux, mem: mem, svc: svc}
}

func (f *fixture) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

type call struct {
	method, path, body string
	bearer             bool         // Authorization: Bearer <adminKey>
	cookie             *http.Cookie // 세션 쿠키
	contentType        string       // 비우면 GET이 아닐 때 application/json, "-"면 보내지 않음
	remote             string       // RemoteAddr
	headers            map[string]string
}

func (f *fixture) do(c call) *httptest.ResponseRecorder {
	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	ct := c.contentType
	if ct == "" && c.method != http.MethodGet {
		ct = "application/json"
	}
	if ct != "" && ct != "-" {
		r.Header.Set("Content-Type", ct)
	}
	if c.bearer {
		r.Header.Set("Authorization", "Bearer "+adminKey)
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.remote != "" {
		r.RemoteAddr = c.remote
	}
	for k, v := range c.headers {
		if k == "Host" { // httptest.NewRequest는 Host 헤더를 r.Host로 옮기지 않는다
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

// admin은 Bearer 키로 부른다.
func (f *fixture) admin(method, path, body string) *httptest.ResponseRecorder {
	return f.do(call{method: method, path: path, body: body, bearer: true})
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (f *fixture) getJSON(path string, v any) {
	f.t.Helper()
	rec := f.get(path)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		f.t.Fatalf("GET %s: %v", path, err)
	}
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), `"error":"`+code+`"`) {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

func result(addr string, notAfter time.Time, status monitor.Status, codes ...string) monitor.Result {
	return monitor.Result{
		Address: addr, CheckedAt: now.Add(-time.Hour), Status: status, Reasons: append([]string{}, codes...),
		TLSVersion: "TLS 1.3", Issuer: "R11", Serial: "0a", NotBefore: notAfter.Add(-90 * day), NotAfter: notAfter,
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true, SANs: []string{},
	}
}

type seeded struct {
	a, b, c    store.Domain
	aTarget    store.Target
	cSubdomain string
}

// seed: a.com 정상(60일 남음), b.com 긴급(3일 남음), c.com 대상 없음 + ACME CNAME 없음.
func (f *fixture) seed() seeded {
	f.t.Helper()
	ctx := context.Background()
	var s seeded
	var err error
	must := func(err error) {
		f.t.Helper()
		if err != nil {
			f.t.Fatal(err)
		}
	}
	s.a, err = f.st.CreateDomain(ctx, "a.com", "main")
	must(err)
	s.b, err = f.st.CreateDomain(ctx, "b.com", "")
	must(err)
	s.aTarget, err = f.st.CreateTarget(ctx, store.NewTarget{DomainID: s.a.ID, Hostname: "www.a.com", Port: 443,
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.1")}})
	must(err)
	bTarget, err := f.st.CreateTarget(ctx, store.NewTarget{DomainID: s.b.ID, Hostname: "b.com", Port: 443,
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.2")}})
	must(err)
	must(f.st.SaveResults(ctx, s.aTarget.ID, []monitor.Result{result("10.0.0.1:443", now.Add(60*day), monitor.StatusOK)}))
	must(f.st.SaveResults(ctx, bTarget.ID, []monitor.Result{result("10.0.0.2:443", now.Add(3*day), monitor.StatusCritical, monitor.CodeCertExpiresSoon)}))

	sub := uuid.New()
	acct, err := f.st.CreateAccount(ctx, store.NewAccount{Domain: "c.com", Username: uuid.New(), KeyHash: "secret-hash", Subdomain: sub,
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")}})
	must(err)
	must(f.st.SetCNAMEStatus(ctx, acct.ID, cnamecheck.StatusMissing, "", now))
	s.c, err = f.st.GetDomain(ctx, acct.DomainID)
	must(err)
	s.cSubdomain = sub.String()
	return s
}

func TestSummary(t *testing.T) {
	f := newFixture(t)
	f.seed()
	var got map[string]any
	f.getJSON("/api/summary", &got)
	want := map[string]float64{"domains": 3, "critical": 1, "warning": 1, "info": 0, "ok": 1, "unknown": 0}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if got["checked_at"] != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Errorf("checked_at = %v", got["checked_at"])
	}
}

type domainItem struct {
	ID               int64      `json:"id"`
	FQDN             string     `json:"fqdn"`
	Memo             string     `json:"memo"`
	Status           string     `json:"status"`
	Reasons          []string   `json:"reasons"`
	EarliestNotAfter *time.Time `json:"earliest_not_after"`
	DaysLeft         *int       `json:"days_left"`
	Targets          int        `json:"targets"`
	ACME             struct {
		Enabled     bool   `json:"enabled"`
		CNAMEStatus string `json:"cname_status"`
	} `json:"acme"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

func TestListDomains(t *testing.T) {
	f := newFixture(t)
	s := f.seed()
	var resp struct{ Domains []domainItem }
	f.getJSON("/api/domains", &resp)
	if len(resp.Domains) != 3 {
		t.Fatalf("domains = %+v", resp.Domains)
	}
	a, b, c := resp.Domains[0], resp.Domains[1], resp.Domains[2]
	if a.ID != s.a.ID || a.Status != "ok" || a.Memo != "main" || a.DaysLeft == nil || *a.DaysLeft != 60 || a.Targets != 1 || a.ACME.Enabled || a.LastCheckedAt == nil {
		t.Errorf("a.com = %+v", a)
	}
	if b.Status != "critical" || len(b.Reasons) != 1 || b.Reasons[0] != monitor.CodeCertExpiresSoon || b.DaysLeft == nil || *b.DaysLeft != 3 ||
		b.EarliestNotAfter == nil || !b.EarliestNotAfter.Equal(now.Add(3*day)) {
		t.Errorf("b.com = %+v", b)
	}
	if c.Status != "warning" || c.Reasons[0] != monitor.CodeCNAMEMissing || !c.ACME.Enabled || c.ACME.CNAMEStatus != "missing" ||
		c.Targets != 0 || c.DaysLeft != nil || c.EarliestNotAfter != nil || c.LastCheckedAt != nil {
		t.Errorf("c.com = %+v", c)
	}

	f.getJSON("/api/domains?status=critical", &resp)
	if len(resp.Domains) != 1 || resp.Domains[0].FQDN != "b.com" {
		t.Errorf("status filter = %+v", resp.Domains)
	}
	expectError(t, f.get("/api/domains?status=bogus"), http.StatusBadRequest, "invalid_status")
}

func TestListDomainsEmpty(t *testing.T) {
	f := newFixture(t)
	rec := f.get("/api/domains")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"domains":[]}` {
		t.Fatalf("empty list = %d %s", rec.Code, rec.Body)
	}
}

func TestGetDomain(t *testing.T) {
	f := newFixture(t)
	s := f.seed()

	var a struct {
		FQDN    string `json:"fqdn"`
		Status  string `json:"status"`
		Targets []struct {
			ID       int64    `json:"id"`
			Hostname string   `json:"hostname"`
			Port     int      `json:"port"`
			IPs      []string `json:"ips"`
			Status   string   `json:"status"`
			Results  []struct {
				Address  string    `json:"address"`
				NotAfter time.Time `json:"not_after"`
			} `json:"results"`
		} `json:"targets"`
		ACME map[string]any `json:"acme"`
	}
	f.getJSON("/api/domains/"+strconv.FormatInt(s.a.ID, 10), &a)
	if a.FQDN != "a.com" || a.Status != "ok" || len(a.Targets) != 1 {
		t.Fatalf("a.com = %+v", a)
	}
	tg := a.Targets[0]
	if tg.ID != s.aTarget.ID || tg.Hostname != "www.a.com" || tg.Port != 443 || len(tg.IPs) != 1 || tg.IPs[0] != "10.0.0.1" ||
		tg.Status != "ok" || len(tg.Results) != 1 || tg.Results[0].Address != "10.0.0.1:443" {
		t.Fatalf("target = %+v", tg)
	}
	if len(a.ACME) != 1 || a.ACME["enabled"] != false {
		t.Fatalf("acme off = %v", a.ACME)
	}

	rec := f.get("/api/domains/" + strconv.FormatInt(s.c.ID, 10))
	body := rec.Body.String()
	if strings.Contains(body, "secret-hash") || strings.Contains(body, "password") || strings.Contains(body, "key_hash") {
		t.Fatalf("secret leaked: %s", body)
	}
	var c struct {
		Status  string `json:"status"`
		Targets []any  `json:"targets"`
		ACME    struct {
			Enabled             bool     `json:"enabled"`
			Subdomain           string   `json:"subdomain"`
			FullDomain          string   `json:"fulldomain"`
			CNAMERecord         string   `json:"cname_record"`
			AllowFrom           []string `json:"allowfrom"`
			CNAMEStatus         string   `json:"cname_status"`
			ConsecutiveFailures *int     `json:"consecutive_failures"`
		} `json:"acme"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	full := s.cSubdomain + ".acme.test"
	if c.Status != "warning" || c.Targets == nil || len(c.Targets) != 0 || !c.ACME.Enabled || c.ACME.Subdomain != s.cSubdomain ||
		c.ACME.FullDomain != full || c.ACME.CNAMERecord != "_acme-challenge.c.com. CNAME "+full+"." ||
		len(c.ACME.AllowFrom) != 1 || c.ACME.AllowFrom[0] != "10.0.1.0/24" || c.ACME.CNAMEStatus != "missing" ||
		c.ACME.ConsecutiveFailures == nil || *c.ACME.ConsecutiveFailures != 0 {
		t.Fatalf("c.com = %+v", c)
	}

	expectError(t, f.get("/api/domains/99999"), http.StatusNotFound, "not_found")
	expectError(t, f.get("/api/domains/abc"), http.StatusNotFound, "not_found")
}

func TestTargetResults(t *testing.T) {
	f := newFixture(t)
	s := f.seed()
	ctx := context.Background()
	for i := range 2 {
		r := result("10.0.0.1:443", now.Add(60*day), monitor.StatusOK)
		r.CheckedAt = now.Add(time.Duration(i+1) * time.Minute)
		if err := f.st.SaveResults(ctx, s.aTarget.ID, []monitor.Result{r}); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/targets/" + strconv.FormatInt(s.aTarget.ID, 10) + "/results"
	var resp struct {
		Results []struct {
			CheckedAt time.Time `json:"checked_at"`
		} `json:"results"`
	}
	f.getJSON(path, &resp)
	if len(resp.Results) != 3 || !resp.Results[0].CheckedAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("results = %+v", resp.Results)
	}
	f.getJSON(path+"?limit=2", &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("limit=2 gave %d", len(resp.Results))
	}
	f.getJSON(path+"?limit=100000", &resp) // 500으로 줄인다
	expectError(t, f.get(path+"?limit=0"), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.get(path+"?limit=x"), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.get("/api/targets/99999/results"), http.StatusNotFound, "not_found")
}

func TestConfig(t *testing.T) {
	f := newFixture(t)
	cfg := func(r *http.Request) map[string]string {
		t.Helper()
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, r)
		var m map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return m
	}
	proxied := func() *http.Request {
		r := httptest.NewRequest("GET", "/api/config", nil)
		r.RemoteAddr = "10.0.0.5:4000"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "ssl.example.com")
		return r
	}

	m := cfg(proxied())
	if m["challenge_zone"] != "acme.test" || m["check_interval"] != "6h0m0s" || m["public_url"] != "https://ssl.example.com" {
		t.Fatalf("proxied config = %v", m)
	}
	direct := proxied()
	direct.RemoteAddr = "192.0.2.9:4000" // 신뢰하지 않는 곳에서 온 X-Forwarded-*는 무시한다
	if m := cfg(direct); m["public_url"] != "http://example.com" {
		t.Fatalf("untrusted forwarded headers used: %v", m)
	}
	f.api.PublicURL = "https://fixed.example.com"
	if m := cfg(proxied()); m["public_url"] != "https://fixed.example.com" {
		t.Fatalf("PUBLIC_URL ignored: %v", m)
	}
}

func TestCodes(t *testing.T) {
	f := newFixture(t)
	var out struct {
		Codes []struct{ Code, Status, Meaning string }
	}
	f.getJSON("/api/codes", &out)
	want := monitor.Codes()
	if len(out.Codes) != len(want) {
		t.Fatalf("codes = %d, want %d", len(out.Codes), len(want))
	}
	if out.Codes[0].Code != want[0].Code || out.Codes[0].Status != "critical" || out.Codes[0].Meaning == "" {
		t.Fatalf("first code = %+v", out.Codes[0])
	}
}

func TestUnknownPathAndMethod(t *testing.T) {
	f := newFixture(t)
	rec := f.get("/api/nope")
	expectError(t, rec, http.StatusNotFound, "not_found")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	for _, path := range []string{"/api/domains", "/api/domains/1", "/api/summary", "/api/targets/1/results", "/api/config", "/api/codes"} {
		rec := f.do(call{method: http.MethodPut, path: path})
		expectError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
			t.Fatalf("%s Allow = %q", path, allow)
		}
	}
	rec = f.do(call{method: http.MethodPost, path: "/api/nope"})
	expectError(t, rec, http.StatusNotFound, "not_found")
	if rec := f.get("/api/config"); rec.Code != http.StatusOK {
		t.Fatalf("config = %d", rec.Code)
	}
}
