package adminapi_test

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/monitor"
)

type detailResp struct {
	ID      int64  `json:"id"`
	FQDN    string `json:"fqdn"`
	Memo    string `json:"memo"`
	Status  string `json:"status"`
	Targets []struct {
		ID      int64    `json:"id"`
		IPs     []string `json:"ips"`
		Results []struct {
			Address string `json:"address"`
		} `json:"results"`
	} `json:"targets"`
	ACME map[string]any `json:"acme"`
}

type targetResp struct {
	ID       int64    `json:"id"`
	Hostname string   `json:"hostname"`
	Port     int      `json:"port"`
	SNI      string   `json:"sni"`
	IPs      []string `json:"ips"`
	Memo     string   `json:"memo"`
	Status   string   `json:"status"`
	Reasons  []string `json:"reasons"`
	Results  []struct {
		Address string `json:"address"`
		Status  string `json:"status"`
		ChainOK bool   `json:"chain_ok"`
	} `json:"results"`
}

// closedPort는 아무도 듣지 않는 로컬 포트다. 접속하면 바로 거부된다.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func (f *fixture) createDomain(fqdn string) detailResp {
	f.t.Helper()
	rec := f.admin("POST", "/api/domains", `{"fqdn":"`+fqdn+`"}`)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("create domain = %d %s", rec.Code, rec.Body)
	}
	return decodeBody[detailResp](f.t, rec)
}

func TestWritesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	for _, c := range []call{
		{method: "POST", path: "/api/domains", body: `{"fqdn":"a.com"}`},
		{method: "PATCH", path: "/api/domains/1", body: `{}`},
		{method: "DELETE", path: "/api/domains/1"},
		{method: "POST", path: "/api/domains/1/targets", body: `{}`},
		{method: "PATCH", path: "/api/targets/1", body: `{}`},
		{method: "DELETE", path: "/api/targets/1"},
		{method: "POST", path: "/api/targets/1/check"},
	} {
		expectError(t, f.do(c), http.StatusUnauthorized, "unauthorized")
	}
}

func TestDomainCRUD(t *testing.T) {
	f := newFixture(t)
	rec := f.admin("POST", "/api/domains", `{"fqdn":" A.com. ","memo":"main"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	d := decodeBody[detailResp](t, rec)
	if d.FQDN != "a.com" || d.Memo != "main" || d.Status != "unknown" || len(d.Targets) != 0 || d.ACME["enabled"] != false {
		t.Fatalf("created = %+v", d)
	}
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"a.com"}`), http.StatusConflict, "domain_exists")
	for _, bad := range []string{`{"fqdn":"*.a.com"}`, `{"fqdn":"com"}`, `{"fqdn":""}`, `{"fqdn":"a b.com"}`} {
		expectError(t, f.admin("POST", "/api/domains", bad), http.StatusBadRequest, "invalid_domain")
	}
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"b.com","memo":"`+strings.Repeat("가", 501)+`"}`), http.StatusBadRequest, "invalid_memo")
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"b.com","bogus":1}`), http.StatusBadRequest, "malformed_json")

	path := fmt.Sprintf("/api/domains/%d", d.ID)
	if got := decodeBody[detailResp](t, f.admin("PATCH", path, `{"memo":"edited"}`)); got.Memo != "edited" {
		t.Fatalf("patched = %+v", got)
	}
	if got := decodeBody[detailResp](t, f.admin("PATCH", path, `{}`)); got.Memo != "edited" {
		t.Fatalf("empty patch changed memo: %+v", got)
	}
	expectError(t, f.admin("PATCH", "/api/domains/99999", `{"memo":"x"}`), http.StatusNotFound, "not_found")

	if rec := f.admin("DELETE", path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	expectError(t, f.get(path), http.StatusNotFound, "not_found")
	expectError(t, f.admin("DELETE", path, ""), http.StatusNotFound, "not_found")

	actions := auditActions(t, f)
	for _, want := range []string{"domain_delete", "domain_update", "domain_create"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}

// ACME가 켜진 도메인을 지우면 공급자의 챌린지 TXT도 지운다.
func TestDeleteDomainDisablesACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	ctx := context.Background()
	reg, err := f.svc.Register(ctx, "a.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := f.svc.Authenticate(ctx, reg.Username, reg.Password)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateTXT(ctx, acct, strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	if rec := f.admin("DELETE", fmt.Sprintf("/api/domains/%d", d.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if v := f.mem.Values(reg.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
	if got := auditDetails(t, f, "acme_disable"); len(got) != 1 || got[0] != "subdomain="+reg.Subdomain {
		t.Fatalf("acme_disable audit = %v", got)
	}
	if got := auditDetails(t, f, "domain_delete"); len(got) != 1 || got[0] != "subdomain="+reg.Subdomain {
		t.Fatalf("domain_delete audit = %v", got)
	}
}

func TestDeleteDomainProviderFailure(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	if _, err := f.svc.Register(context.Background(), "a.com", nil); err != nil {
		t.Fatal(err)
	}
	f.mem.SetFailure(errors.New("cloudflare down"))
	expectError(t, f.admin("DELETE", fmt.Sprintf("/api/domains/%d", d.ID), ""), http.StatusInternalServerError, "dns_provider_error")
	f.mem.SetFailure(nil)
	if got := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID))); got.ACME["enabled"] != true {
		t.Fatalf("domain or acme lost: %+v", got)
	}
}

// 대상을 만들면 그 자리에서 점검하고 결과를 돌려준다. 접속이 안 되는 IP가 여러 개여도 빨리 끝난다.
func TestCreateTargetChecksImmediately(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	port := closedPort(t)
	body := fmt.Sprintf(`{"hostname":"WWW.a.com.","port":%d,"ips":["127.0.0.1"," 127.0.0.1 ","::ffff:127.0.0.1"],"memo":"web"}`, port)
	start := time.Now()
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), body)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("create took %s", time.Since(start))
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target = %d %s", rec.Code, rec.Body)
	}
	tg := decodeBody[targetResp](t, rec)
	if tg.Hostname != "www.a.com" || tg.Port != port || tg.Memo != "web" || len(tg.IPs) != 1 || tg.IPs[0] != "127.0.0.1" {
		t.Fatalf("target = %+v (IPs should be trimmed, unmapped and deduplicated)", tg)
	}
	if len(tg.Results) != 1 || tg.Results[0].Status != "critical" || tg.Status != "critical" || tg.Reasons[0] != monitor.CodeConnectFailed {
		t.Fatalf("results = %+v", tg)
	}
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), body), http.StatusConflict, "target_exists")
	expectError(t, f.admin("POST", "/api/domains/99999/targets", body), http.StatusNotFound, "not_found")
}

func TestCreateTargetValidation(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	path := fmt.Sprintf("/api/domains/%d/targets", d.ID)
	var seventeen []string
	for i := range 17 {
		seventeen = append(seventeen, fmt.Sprintf(`"10.0.0.%d"`, i+1))
	}
	many := strings.Join(seventeen, ",")
	for _, bad := range []string{
		`{}`,
		`{"hostname":""}`,
		`{"hostname":"*.a.com"}`,
		`{"hostname":"a..com"}`,
		`{"hostname":"a.com","port":0}`,
		`{"hostname":"a.com","port":65536}`,
		`{"hostname":"a.com","sni":"10.0.0.1"}`,
		`{"hostname":"a.com","sni":"*.a.com"}`,
		`{"hostname":"a.com","ips":["not-an-ip"]}`,
		`{"hostname":"a.com","ips":["fe80::1%eth0"]}`,
		`{"hostname":"a.com","ips":[` + many + `]}`,
		`{"hostname":"a.com","memo":"` + strings.Repeat("x", 501) + `"}`,
	} {
		expectError(t, f.admin("POST", path, bad), http.StatusBadRequest, "invalid_target")
	}
	// 내부 이름(한 단어)과 IP 리터럴 호스트명은 받는다.
	for _, ok := range []string{`{"hostname":"intranet","ips":["127.0.0.1"],"port":1}`, `{"hostname":"10.0.0.9","ips":["127.0.0.1"],"port":2}`} {
		if rec := f.admin("POST", path, ok); rec.Code != http.StatusCreated {
			t.Errorf("%s = %d %s", ok, rec.Code, rec.Body)
		}
	}
}

// 실제 TLS 서버를 대상으로 만들면 결과가 정상으로 나온다.
func TestCreateTargetAgainstTLSServer(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	f.api.Runner.Prober.Roots = roots
	d := f.createDomain("example.com")
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), fmt.Sprintf(`{"hostname":"example.com","port":%d,"ips":["127.0.0.1"]}`, port))
	tg := decodeBody[targetResp](t, rec)
	if rec.Code != http.StatusCreated || len(tg.Results) != 1 || !tg.Results[0].ChainOK || tg.Results[0].Status == "critical" {
		t.Fatalf("target = %d %+v", rec.Code, tg)
	}
}

// 접속 설정을 바꾸면 그 자리에서 다시 점검해, 옛 IP 결과가 섞이지 않는다.
func TestPatchTargetRechecks(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	port := closedPort(t)
	created := decodeBody[targetResp](t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID),
		fmt.Sprintf(`{"hostname":"www.a.com","port":%d,"ips":["127.0.0.1"]}`, port)))
	path := fmt.Sprintf("/api/targets/%d", created.ID)

	rec := f.admin("PATCH", path, `{"ips":["127.0.0.2"]}`)
	got := decodeBody[targetResp](t, rec)
	want := fmt.Sprintf("127.0.0.2:%d", port)
	if rec.Code != http.StatusOK || len(got.Results) != 1 || got.Results[0].Address != want {
		t.Fatalf("patched = %d %+v", rec.Code, got)
	}
	detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID)))
	if len(detail.Targets) != 1 || len(detail.Targets[0].Results) != 1 || detail.Targets[0].Results[0].Address != want {
		t.Fatalf("detail after patch = %+v", detail.Targets)
	}

	// 메모만 바꾸면 다시 점검하지 않는다(결과 개수가 그대로).
	before := len(history(t, f, created.ID))
	if rec := f.admin("PATCH", path, `{"memo":"note"}`); rec.Code != http.StatusOK || decodeBody[targetResp](t, rec).Memo != "note" {
		t.Fatalf("memo patch = %d %s", rec.Code, rec.Body)
	}
	if after := len(history(t, f, created.ID)); after != before {
		t.Fatalf("memo-only patch ran a check: %d -> %d", before, after)
	}
	expectError(t, f.admin("PATCH", path, `{"port":0}`), http.StatusBadRequest, "invalid_target")
	expectError(t, f.admin("PATCH", "/api/targets/99999", `{"memo":"x"}`), http.StatusNotFound, "not_found")
}

func TestCheckAndDeleteTarget(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	created := decodeBody[targetResp](t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID),
		fmt.Sprintf(`{"hostname":"www.a.com","port":%d,"ips":["127.0.0.1"]}`, closedPort(t))))
	rec := f.admin("POST", fmt.Sprintf("/api/targets/%d/check", created.ID), "")
	resp := decodeBody[struct {
		Results []struct{ Status string } `json:"results"`
	}](t, rec)
	if rec.Code != http.StatusOK || len(resp.Results) != 1 {
		t.Fatalf("check = %d %s", rec.Code, rec.Body)
	}
	if n := len(history(t, f, created.ID)); n != 2 {
		t.Fatalf("history = %d, want 2 (create + check)", n)
	}
	expectError(t, f.admin("POST", "/api/targets/99999/check", ""), http.StatusNotFound, "not_found")
	if rec := f.admin("DELETE", fmt.Sprintf("/api/targets/%d", created.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	expectError(t, f.admin("DELETE", fmt.Sprintf("/api/targets/%d", created.ID), ""), http.StatusNotFound, "not_found")
	actions := auditActions(t, f)
	for _, want := range []string{"target_create", "target_check", "target_delete"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}

func history(t *testing.T, f *fixture, targetID int64) []any {
	t.Helper()
	return decodeBody[struct {
		Results []any `json:"results"`
	}](t, f.get(fmt.Sprintf("/api/targets/%d/results", targetID))).Results
}

func auditActions(t *testing.T, f *fixture) []string {
	t.Helper()
	resp := decodeBody[struct {
		Entries []struct{ Action string } `json:"entries"`
	}](t, f.admin("GET", "/api/audit", ""))
	out := make([]string, len(resp.Entries))
	for i, e := range resp.Entries {
		out[i] = e.Action
	}
	return out
}

// auditDetails는 action별 감사 기록의 detail이다(최신순).
func auditDetails(t *testing.T, f *fixture, action string) []string {
	t.Helper()
	resp := decodeBody[struct {
		Entries []struct{ Action, Detail string } `json:"entries"`
	}](t, f.admin("GET", "/api/audit", ""))
	var out []string
	for _, e := range resp.Entries {
		if e.Action == action {
			out = append(out, e.Detail)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
