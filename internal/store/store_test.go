package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/store"
)

func newStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	pool := dbtest.New(t)
	return store.New(pool), pool
}

func create(t *testing.T, st *store.Store, domain string, allow ...netip.Prefix) store.Account {
	t.Helper()
	a, err := st.CreateAccount(context.Background(), store.NewAccount{
		Domain: domain, Username: uuid.New(), KeyHash: "hash", Subdomain: uuid.New(), AllowFrom: allow,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return a
}

func TestCreateAndLoadAccount(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	allow := []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16"), netip.MustParsePrefix("2001:db8::/32")}
	a := create(t, st, "a.com", allow...)

	got, err := st.ActiveAccountByUsername(ctx, a.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Domain != "a.com" || got.Subdomain != a.Subdomain || got.KeyHash != "hash" {
		t.Fatalf("got %+v, want %+v", got, a)
	}
	if !slices.Equal(got.AllowFrom, allow) {
		t.Fatalf("allowfrom = %v", got.AllowFrom)
	}
}

func TestCreateAccountWithoutAllowFrom(t *testing.T) {
	st, _ := newStore(t)
	a := create(t, st, "a.com")
	got, err := st.ActiveAccountByUsername(context.Background(), a.Username)
	if err != nil || len(got.AllowFrom) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOneActiveAccountPerDomain(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	create(t, st, "a.com")
	_, err := st.CreateAccount(ctx, store.NewAccount{Domain: "a.com", Username: uuid.New(), KeyHash: "h", Subdomain: uuid.New()})
	if !errors.Is(err, store.ErrActiveAccountExists) {
		t.Fatalf("err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	create(t, st, "a.com")
}

func TestActiveAccountByUsernameNotFound(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	revoked := create(t, st, "a.com")
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"not-a-uuid", "", uuid.NewString(), revoked.Username} {
		if _, err := st.ActiveAccountByUsername(ctx, name); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("ActiveAccountByUsername(%q) err = %v", name, err)
		}
	}
}

func TestUpdateTXT(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")

	set := func(want []string) {
		t.Helper()
		if err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return want, nil }); err != nil {
			t.Fatal(err)
		}
	}
	current := func() []string {
		t.Helper()
		var got []string
		_ = st.UpdateTXT(ctx, a.ID, func(cur []string) ([]string, error) { got = cur; return cur, nil })
		return got
	}

	set([]string{"v1"})
	set([]string{"v1", "v2"})
	if got := current(); !slices.Equal(got, []string{"v1", "v2"}) {
		t.Fatalf("after two: %v", got)
	}
	set([]string{"v2", "v3"})
	if got := current(); !slices.Equal(got, []string{"v2", "v3"}) {
		t.Fatalf("oldest-first order after replace: %v", got)
	}

	boom := errors.New("provider down")
	err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := current(); !slices.Equal(got, []string{"v2", "v3"}) {
		t.Fatalf("fn error changed values: %v", got)
	}

	set(nil)
	if got := current(); len(got) != 0 {
		t.Fatalf("nil set should clear: %v", got)
	}
}

func TestUpdateTXTOrderFollowsInsertNotTxStart(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")

	// tx A의 now()는 시작 시점에 고정된다.
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(ctx)
	var started time.Time
	if err := txA.QueryRow(ctx, `SELECT now()`).Scan(&started); err != nil {
		t.Fatal(err)
	}

	if err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return []string{"v1"}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := txA.Exec(ctx, `INSERT INTO txt_records (account_id, value) VALUES ($1, 'v2')`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var got []string
	if err := st.UpdateTXT(ctx, a.ID, func(cur []string) ([]string, error) { got = cur; return cur, nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"v1", "v2"}) {
		t.Fatalf("order = %v, want [v1 v2] (insert order, not tx start)", got)
	}
}

func TestUpdateTXTRevoked(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	called := false
	err := st.UpdateTXT(ctx, a.ID, func(cur []string) ([]string, error) { called = true; return cur, nil })
	if !errors.Is(err, store.ErrNotFound) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestActiveTXTBySubdomain(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	withValues := create(t, st, "a.com")
	empty := create(t, st, "b.com")
	revoked := create(t, st, "c.com")
	if err := st.UpdateTXT(ctx, withValues.ID, func([]string) ([]string, error) { return []string{"x", "y"}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.ActiveTXTBySubdomain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries: %v", len(got), got)
	}
	if e := got[withValues.Subdomain]; e.AccountID != withValues.ID || !slices.Equal(e.Values, []string{"x", "y"}) {
		t.Fatalf("withValues = %+v", e)
	}
	if e, ok := got[empty.Subdomain]; !ok || e.Values == nil || len(e.Values) != 0 {
		t.Fatalf("empty = %+v, %v", e, ok)
	}
}

func TestAudit(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	if err := st.Audit(ctx, store.AuditEntry{Actor: "admin", Action: "register", Target: "a.com", SourceIP: "10.0.0.1", Detail: "d"}); err != nil {
		t.Fatal(err)
	}
	var actor, action, target, ip, detail string
	err := pool.QueryRow(ctx, `SELECT actor, action, target, source_ip, detail FROM audit_logs`).Scan(&actor, &action, &target, &ip, &detail)
	if err != nil || actor != "admin" || action != "register" || target != "a.com" || ip != "10.0.0.1" || detail != "d" {
		t.Fatalf("row = %s %s %s %s %s, %v", actor, action, target, ip, detail, err)
	}
}
