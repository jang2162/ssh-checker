package cleanup_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/store"
)

const zone = "acme-chk.test"

func txt(c byte) string { return strings.Repeat(string(c), 43) }

type env struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	st   *store.Store
	mem  *memory.Provider
	svc  *acmedns.Service
	c    *cleanup.Cleaner
}

func newEnv(t *testing.T) *env {
	pool := dbtest.New(t)
	st := store.New(pool)
	mem := memory.New()
	return &env{
		t: t, ctx: context.Background(), pool: pool, st: st, mem: mem,
		svc: &acmedns.Service{Store: st, Provider: mem, Zone: zone, TTL: 60},
		c:   &cleanup.Cleaner{Store: st, Provider: mem, Zone: zone, TTL: 60, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
}

// account는 계정을 만들고 DB에 dbVals를, 공급자에 provVals를 직접 넣는다.
func (e *env) account(domain string, dbVals, provVals []string) store.Account {
	e.t.Helper()
	reg, err := e.svc.Register(e.ctx, domain, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	acct, err := e.st.ActiveAccountByUsername(e.ctx, reg.Username)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.UpdateTXT(e.ctx, acct.ID, func([]string) ([]string, error) { return dbVals, nil }); err != nil {
		e.t.Fatal(err)
	}
	if err := e.mem.SetTXT(e.ctx, reg.FullDomain, provVals, 60); err != nil {
		e.t.Fatal(err)
	}
	return acct
}

func (e *env) values(label string) []string { return e.mem.Values(label + "." + zone) }

func TestCleanup(t *testing.T) {
	e := newEnv(t)
	inSync := e.account("insync.com", []string{txt('a')}, []string{txt('a')})
	lost := e.account("lost.com", nil, []string{txt('y')})
	stale := e.account("stale.com", []string{txt('a'), txt('b')}, []string{txt('a'), txt('z')})
	revoked := e.account("revoked.com", []string{txt('r')}, []string{txt('r')})
	if _, err := e.pool.Exec(e.ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	orphan := uuid.NewString()
	if err := e.mem.SetTXT(e.ctx, orphan+"."+zone, []string{txt('o')}, 60); err != nil {
		t.Fatal(err)
	}

	res, err := e.c.Run(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 2 || res.Repaired != 2 {
		t.Fatalf("result = %+v, want 2 deleted (revoked, orphan) and 2 repaired (lost, stale)", res)
	}
	checks := map[string][]string{
		inSync.Subdomain:  {txt('a')},
		lost.Subdomain:    {},
		stale.Subdomain:   {txt('a'), txt('b')},
		revoked.Subdomain: {},
		orphan:            {},
	}
	for label, want := range checks {
		got := e.values(label)
		if got == nil {
			got = []string{}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", label, got, want)
		}
	}
	var n int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM audit_logs WHERE action = 'txt_cleanup'`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("audit rows = %d, %v", n, err)
	}

	again, err := e.c.Run(e.ctx)
	if err != nil || again != (cleanup.Result{}) {
		t.Fatalf("second run = %+v, %v; want no changes", again, err)
	}
}

func TestCleanupLeavesNonUUIDNames(t *testing.T) {
	e := newEnv(t)
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone, "8E5700EA-A4BF-41C7-8A77-E990661DCC6A-x." + zone} {
		if err := e.mem.SetTXT(e.ctx, fqdn, []string{"keep"}, 60); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.c.Run(e.ctx)
	if err != nil || res != (cleanup.Result{}) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone} {
		if got := e.mem.Values(fqdn); !slices.Equal(got, []string{"keep"}) {
			t.Errorf("%s = %v; cleanup touched a non-challenge record", fqdn, got)
		}
	}
}
