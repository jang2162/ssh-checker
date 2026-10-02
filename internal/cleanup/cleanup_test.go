package cleanup_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone, "8E5700EA-A4BF-41C7-8A77-E990661DCC6A-x." + zone, "gggggggg-gggg-gggg-gggg-gggggggggggg" + "." + zone} {
		if err := e.mem.SetTXT(e.ctx, fqdn, []string{"keep"}, 60); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.c.Run(e.ctx)
	if err != nil || res != (cleanup.Result{}) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone, "gggggggg-gggg-gggg-gggg-gggggggggggg" + "." + zone} {
		if got := e.mem.Values(fqdn); !slices.Equal(got, []string{"keep"}) {
			t.Errorf("%s = %v; cleanup touched a non-challenge record", fqdn, got)
		}
	}
}

func TestCleanupStopsOnCancelledContext(t *testing.T) {
	e := newEnv(t)
	orphan := uuid.NewString()
	if err := e.mem.SetTXT(e.ctx, orphan+"."+zone, []string{txt('o')}, 60); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	cancel()
	res, err := e.c.Run(ctx)
	if err == nil {
		t.Fatal("Run with cancelled context returned nil error")
	}
	if res != (cleanup.Result{}) {
		t.Fatalf("result = %+v, want no changes", res)
	}
	if got := e.values(orphan); !slices.Equal(got, []string{txt('o')}) {
		t.Fatalf("orphan = %v; cancelled run must not delete", got)
	}
}

// 진행 중인 /update가 공급자에 쓴 값(아직 커밋 전)은 동시에 도는 정리가 지우면 안 된다.
func TestCleanupWaitsForInFlightUpdate(t *testing.T) {
	e := newEnv(t)
	acct := e.account("inflight.com", nil, nil)
	fqdn := acct.Subdomain + "." + zone
	started, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	var updErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		updErr = e.st.UpdateTXT(e.ctx, acct.ID, func([]string) ([]string, error) {
			if err := e.mem.SetTXT(e.ctx, fqdn, []string{txt('n')}, 60); err != nil {
				return nil, err
			}
			close(started)
			<-release
			return []string{txt('n')}, nil
		})
	}()
	<-started

	done := make(chan error, 1)
	go func() {
		_, err := e.c.Run(e.ctx)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Run returned %v while /update held the row lock", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if updErr != nil {
		t.Fatal(updErr)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := e.values(acct.Subdomain); !slices.Equal(got, []string{txt('n')}) {
		t.Errorf("provider = %v, want the in-flight value kept", got)
	}
	var vals []string
	if err := e.st.UpdateTXT(e.ctx, acct.ID, func(cur []string) ([]string, error) { vals = cur; return cur, nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(vals, []string{txt('n')}) {
		t.Errorf("db = %v, want committed value", vals)
	}
}
