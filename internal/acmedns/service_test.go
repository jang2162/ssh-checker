package acmedns_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/store"
)

const testZone = "acme-chk.test"

func txt(c byte) string { return strings.Repeat(string(c), 43) }

func newService(t *testing.T) (*acmedns.Service, *memory.Provider, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.New(t)
	mem := memory.New()
	return &acmedns.Service{Store: store.New(pool), Provider: mem, Zone: testZone, TTL: 60}, mem, pool
}

func dbValues(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT value FROM txt_records ORDER BY value`)
	if err != nil {
		t.Fatal(err)
	}
	vals, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return vals
}

func registerAndAuth(t *testing.T, svc *acmedns.Service, domain string) (acmedns.Registration, store.Account) {
	t.Helper()
	ctx := context.Background()
	reg, err := svc.Register(ctx, domain, nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	acct, err := svc.Authenticate(ctx, reg.Username, reg.Password)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return reg, acct
}

func TestRegister(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	allow := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	reg, err := svc.Register(ctx, " A.com. ", allow)
	if err != nil {
		t.Fatal(err)
	}
	if !acmedns.ValidSubdomain(reg.Subdomain) || reg.FullDomain != reg.Subdomain+"."+testZone {
		t.Fatalf("registration = %+v", reg)
	}
	if len(reg.Password) != 40 || reg.Username == reg.Subdomain {
		t.Fatalf("credentials = %+v", reg)
	}
	if _, err := svc.Register(ctx, "a.com", nil); !errors.Is(err, store.ErrActiveAccountExists) {
		t.Fatalf("duplicate: err = %v", err)
	}
	for _, bad := range []string{"", "com", "*.a.com", "a..com"} {
		if _, err := svc.Register(ctx, bad, nil); !errors.Is(err, acmedns.ErrInvalidDomain) {
			t.Errorf("Register(%q) err = %v", bad, err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if acct.Subdomain != reg.Subdomain || acct.Domain != "a.com" {
		t.Fatalf("account = %+v", acct)
	}
	cases := map[string][2]string{
		"wrong key":      {reg.Username, "wrong"},
		"empty key":      {reg.Username, ""},
		"long key":       {reg.Username, strings.Repeat("k", 100)},
		"unknown user":   {"11111111-1111-1111-1111-111111111111", reg.Password},
		"non-uuid user":  {"abc", reg.Password},
		"empty username": {"", ""},
	}
	for name, c := range cases {
		if _, err := svc.Authenticate(ctx, c[0], c[1]); !errors.Is(err, acmedns.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestUpdateTXTRotates(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	fqdn := svc.FQDN(acct.Subdomain)
	for _, v := range []string{txt('a'), txt('b'), txt('c')} {
		if err := svc.UpdateTXT(ctx, acct, v); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{txt('b'), txt('c')}
	if got := mem.Values(fqdn); !slices.Equal(got, want) {
		t.Fatalf("provider = %v", got)
	}
	if got := dbValues(t, pool); !slices.Equal(got, want) {
		t.Fatalf("db = %v", got)
	}
}

func TestUpdateTXTRejectsBadValue(t *testing.T) {
	svc, mem, _ := newService(t)
	_, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(context.Background(), acct, "short"); !errors.Is(err, acmedns.ErrBadTXT) {
		t.Fatalf("err = %v", err)
	}
	if got := mem.Values(svc.FQDN(acct.Subdomain)); len(got) != 0 {
		t.Fatalf("provider written: %v", got)
	}
}

func TestUpdateTXTProviderFailureLeavesDBUnchanged(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	mem.SetFailure(errors.New("cloudflare down"))
	err := svc.UpdateTXT(ctx, acct, txt('b'))
	var perr *dnsprovider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("err = %v, want *dnsprovider.Error", err)
	}
	if got := dbValues(t, pool); !slices.Equal(got, []string{txt('a')}) {
		t.Fatalf("db changed on provider failure: %v", got)
	}
	mem.SetFailure(nil)
	if err := svc.UpdateTXT(ctx, acct, txt('b')); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := dbValues(t, pool); !slices.Equal(got, []string{txt('a'), txt('b')}) {
		t.Fatalf("db after retry: %v", got)
	}
}

func TestUpdateTXTConcurrent(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	values := []string{txt('a'), txt('b'), txt('c'), txt('d'), txt('e')}
	var wg sync.WaitGroup
	errs := make(chan error, len(values))
	for _, v := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.UpdateTXT(ctx, acct, v)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	db := dbValues(t, pool)
	prov := mem.Values(svc.FQDN(acct.Subdomain))
	if len(db) != 2 || !slices.Equal(db, prov) {
		t.Fatalf("db = %v, provider = %v; want the same 2 values", db, prov)
	}
}

func TestRotate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	rot, err := svc.Rotate(ctx, acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rot.Username != reg.Username || rot.Subdomain != reg.Subdomain || rot.FullDomain != reg.FullDomain ||
		rot.Password == reg.Password || len(rot.Password) != 40 {
		t.Fatalf("Rotate = %+v (was %+v)", rot, reg)
	}
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("old key = %v", err)
	}
	if _, err := svc.Authenticate(ctx, rot.Username, rot.Password); err != nil {
		t.Fatalf("new key = %v", err)
	}
	if _, err := svc.Rotate(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestDisable(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	if len(mem.Values(reg.FullDomain)) != 1 {
		t.Fatal("setup: txt not written")
	}
	if err := svc.Disable(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if v := mem.Values(reg.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
	if v := dbValues(t, pool); len(v) != 0 {
		t.Fatalf("db still has %v", v)
	}
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("revoked account authenticated: %v", err)
	}
	if err := svc.Disable(ctx, acct.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second disable = %v", err)
	}
}

func TestDisableProviderFailureKeepsAccount(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	mem.SetFailure(errors.New("cloudflare down"))
	err := svc.Disable(ctx, acct.ID)
	var perr *dnsprovider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("Disable = %v, want provider error", err)
	}
	mem.SetFailure(nil)
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); err != nil {
		t.Fatalf("account lost after failed disable: %v", err)
	}
	if v := dbValues(t, pool); len(v) != 1 {
		t.Fatalf("db txt = %v", v)
	}
}
