package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"ssl-checker/internal/store"
)

func TestRotateKey(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	allow := netip.MustParsePrefix("10.0.0.0/24")
	a := create(t, st, "a.com", allow)
	got, err := st.RotateKey(ctx, a.ID, "new-hash")
	if err != nil || got.ID != a.ID || got.Username != a.Username || got.Subdomain != a.Subdomain || got.Domain != "a.com" ||
		got.KeyHash != "new-hash" || !slices.Equal(got.AllowFrom, []netip.Prefix{allow}) {
		t.Fatalf("RotateKey = %+v, %v", got, err)
	}
	loaded, _ := st.ActiveAccountByUsername(ctx, a.Username)
	if loaded.KeyHash != "new-hash" {
		t.Fatalf("stored hash = %q", loaded.KeyHash)
	}
	if _, err := st.RotateKey(ctx, 99999, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestRevokeAccount(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	if err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return []string{"v1"}, nil }); err != nil {
		t.Fatal(err)
	}

	// beforeCommit이 실패하면 아무것도 바뀌지 않는다.
	boom := errors.New("provider down")
	var gotSub string
	err := st.RevokeAccount(ctx, a.ID, func(_ context.Context, sub string) error { gotSub = sub; return boom })
	if !errors.Is(err, boom) || gotSub != a.Subdomain {
		t.Fatalf("failing revoke = %v (sub %q)", err, gotSub)
	}
	if _, err := st.ActiveAccountByUsername(ctx, a.Username); err != nil {
		t.Fatalf("account gone after failed revoke: %v", err)
	}

	if err := st.RevokeAccount(ctx, a.ID, func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ActiveAccountByUsername(ctx, a.Username); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("account still active: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM txt_records`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("txt rows = %d, %v", n, err)
	}
	if err := st.RevokeAccount(ctx, a.ID, func(context.Context, string) error { return nil }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke = %v", err)
	}
	// 폐기 후 다시 켜면 새 계정을 만들 수 있다.
	create(t, st, "a.com")
}

func TestSetAllowFrom(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	want := []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16"), netip.MustParsePrefix("2001:db8::/32")}
	if err := st.SetAllowFrom(ctx, a.ID, want); err != nil {
		t.Fatal(err)
	}
	s, _ := st.ACMEStateByDomain(ctx, a.DomainID)
	if !slices.Equal(s.AllowFrom, want) {
		t.Fatalf("allowfrom = %v", s.AllowFrom)
	}
	if err := st.SetAllowFrom(ctx, a.ID, nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.ACMEStateByDomain(ctx, a.DomainID); len(s.AllowFrom) != 0 {
		t.Fatalf("cleared allowfrom = %v", s.AllowFrom)
	}
	if err := st.SetAllowFrom(ctx, 99999, want); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}
