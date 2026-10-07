package adminapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
)

type credsResp struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Subdomain   string   `json:"subdomain"`
	FullDomain  string   `json:"fulldomain"`
	CNAMERecord string   `json:"cname_record"`
	AllowFrom   []string `json:"allowfrom"`
	AcmeSh      string   `json:"acme_sh"`
}

func (f *fixture) enableACME(domainID int64, body string) credsResp {
	f.t.Helper()
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", domainID), body)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("enable = %d %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		f.t.Fatalf("Cache-Control = %q", cc)
	}
	return decodeBody[credsResp](f.t, rec)
}

func TestACMEWritesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	for _, c := range []call{
		{method: "POST", path: "/api/domains/1/acme", body: `{}`},
		{method: "PATCH", path: "/api/domains/1/acme", body: `{}`},
		{method: "POST", path: "/api/domains/1/acme/rotate"},
		{method: "POST", path: "/api/domains/1/acme/check"},
		{method: "DELETE", path: "/api/domains/1/acme"},
	} {
		expectError(t, f.do(c), http.StatusUnauthorized, "unauthorized")
	}
}

func TestEnableACME(t *testing.T) {
	f := newFixture(t)
	f.api.PublicURL = "https://ssl.example.com"
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{"allowfrom":[" 10.0.1.0/24 ","10.0.2.5"]}`)
	full := c.Subdomain + ".acme.test"
	if c.FullDomain != full || c.CNAMERecord != "_acme-challenge.a.com. CNAME "+full+"." || len(c.Password) != 40 ||
		len(c.AllowFrom) != 2 || c.AllowFrom[0] != "10.0.1.0/24" || c.AllowFrom[1] != "10.0.2.5/32" {
		t.Fatalf("creds = %+v", c)
	}
	for _, want := range []string{
		`export ACMEDNS_BASE_URL="https://ssl.example.com"`,
		`export ACMEDNS_USERNAME="` + c.Username + `"`,
		`export ACMEDNS_PASSWORD="` + c.Password + `"`,
		`export ACMEDNS_SUBDOMAIN="` + c.Subdomain + `"`,
		`--set-default-ca --server letsencrypt`,
		`--issue --dns dns_acmedns -d a.com -d '*.a.com'`,
		`sudo mkdir -p /etc/ssl/a.com`,
		`--install-cert -d a.com --ecc`,
	} {
		if !strings.Contains(c.AcmeSh, want) {
			t.Errorf("acme_sh missing %q:\n%s", want, c.AcmeSh)
		}
	}
	if strings.Contains(c.AcmeSh, "--force") {
		t.Errorf("enable script must not force:\n%s", c.AcmeSh)
	}
	if got := auditDetails(t, f, "acme_enable"); len(got) != 1 || got[0] != "subdomain="+c.Subdomain+" allowfrom=10.0.1.0/24,10.0.2.5/32" {
		t.Errorf("acme_enable audit = %v", got)
	}
	if _, err := f.svc.Authenticate(context.Background(), c.Username, c.Password); err != nil {
		t.Fatalf("issued credentials do not authenticate: %v", err)
	}
	detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID)))
	if detail.ACME["enabled"] != true || detail.ACME["username"] != c.Username || strings.Contains(fmt.Sprint(detail.ACME), c.Password) {
		t.Fatalf("detail acme = %v", detail.ACME)
	}
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", d.ID), `{}`), http.StatusConflict, "acme_enabled")
	expectError(t, f.admin("POST", "/api/domains/99999/acme", `{}`), http.StatusNotFound, "not_found")
	other := f.createDomain("b.com")
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", other.ID), `{"allowfrom":["nope"]}`), http.StatusBadRequest, "invalid_allowfrom")
	if !contains(auditActions(t, f), "acme_enable") {
		t.Error("audit missing acme_enable")
	}
}

// PUBLIC_URL이 없고 요청에서도 주소를 만들 수 없으면 안내에 자리표시자를 넣는다.
func TestEnableACMEWithoutPublicURL(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	rec := f.do(call{method: "POST", path: fmt.Sprintf("/api/domains/%d/acme", d.ID), body: `{}`, bearer: true, headers: map[string]string{"Host": "bad host"}})
	c := decodeBody[credsResp](t, rec)
	if !strings.Contains(c.AcmeSh, `export ACMEDNS_BASE_URL="<ssl-checker 주소>"`) {
		t.Fatalf("acme_sh = %s", c.AcmeSh)
	}
}

func TestRotateKeepsSubdomain(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	old := f.enableACME(d.ID, `{}`)
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme/rotate", d.ID), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	c := decodeBody[credsResp](t, rec)
	if c.Username != old.Username || c.Subdomain != old.Subdomain || c.CNAMERecord != old.CNAMERecord || c.Password == old.Password {
		t.Fatalf("rotated = %+v, old = %+v", c, old)
	}
	for _, want := range []string{"--issue --dns dns_acmedns -d a.com -d '*.a.com' --force\n", "sudo mkdir -p /etc/ssl/a.com"} {
		if !strings.Contains(c.AcmeSh, want) {
			t.Errorf("rotate script missing %q:\n%s", want, c.AcmeSh)
		}
	}
	ctx := context.Background()
	if _, err := f.svc.Authenticate(ctx, old.Username, old.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("old key still works: %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, c.Username, c.Password); err != nil {
		t.Fatalf("new key = %v", err)
	}
}

func TestPatchACMEAllowFrom(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	f.enableACME(d.ID, `{"allowfrom":["10.0.1.0/24"]}`)
	path := fmt.Sprintf("/api/domains/%d/acme", d.ID)
	rec := f.admin("PATCH", path, `{"allowfrom":[" 10.9.0.0/16"]}`)
	got := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || fmt.Sprint(got["allowfrom"]) != "[10.9.0.0/16]" || got["enabled"] != true {
		t.Fatalf("patch = %d %v", rec.Code, got)
	}
	if got := decodeBody[map[string]any](t, f.admin("PATCH", path, `{"allowfrom":[]}`)); fmt.Sprint(got["allowfrom"]) != "[]" {
		t.Fatalf("cleared = %v", got)
	}
	if got := auditDetails(t, f, "acme_update"); len(got) != 2 || got[1] != "allowfrom=10.9.0.0/16" || got[0] != "allowfrom=" {
		t.Fatalf("acme_update audit = %v", got)
	}
	expectError(t, f.admin("PATCH", path, `{}`), http.StatusBadRequest, "invalid_allowfrom")
	expectError(t, f.admin("PATCH", path, `{"allowfrom":["x"]}`), http.StatusBadRequest, "invalid_allowfrom")
}

func TestCheckACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{}`)
	f.api.CNAME = &cnamecheck.Checker{Resolver: cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.a.com.": {CNAME: c.FullDomain + "."},
	})}
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme/check", d.ID), "")
	got := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || got["cname_status"] != "ok" || got["cname_value"] != c.FullDomain || got["cname_checked_at"] == nil {
		t.Fatalf("check = %d %v", rec.Code, got)
	}
}

func TestDisableACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{}`)
	ctx := context.Background()
	acct, _ := f.svc.Authenticate(ctx, c.Username, c.Password)
	if err := f.svc.UpdateTXT(ctx, acct, strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/domains/%d/acme", d.ID)

	f.mem.SetFailure(errors.New("cloudflare down"))
	expectError(t, f.admin("DELETE", path, ""), http.StatusInternalServerError, "dns_provider_error")
	f.mem.SetFailure(nil)
	if _, err := f.svc.Authenticate(ctx, c.Username, c.Password); err != nil {
		t.Fatalf("account lost after failed disable: %v", err)
	}

	if rec := f.admin("DELETE", path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body)
	}
	if v := f.mem.Values(c.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
	if detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID))); detail.ACME["enabled"] != false {
		t.Fatalf("acme still on: %v", detail.ACME)
	}
	for _, cl := range []call{
		{method: "DELETE", path: path},
		{method: "PATCH", path: path, body: `{"allowfrom":[]}`},
		{method: "POST", path: path + "/rotate"},
		{method: "POST", path: path + "/check"},
	} {
		cl.bearer = true
		expectError(t, f.do(cl), http.StatusConflict, "acme_disabled")
	}
	// 다시 켜면 새 서브도메인이 생긴다.
	if again := f.enableACME(d.ID, `{}`); again.Subdomain == c.Subdomain {
		t.Fatal("re-enable reused the old subdomain")
	}
	actions := auditActions(t, f)
	for _, want := range []string{"acme_disable", "acme_enable"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}
