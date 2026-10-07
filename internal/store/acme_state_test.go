package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/store"
)

func TestACMEStates(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	allow := netip.MustParsePrefix("10.0.0.0/24")
	a := create(t, st, "a.com", allow)

	states, err := st.ActiveACMEStates(ctx)
	if err != nil || len(states) != 1 {
		t.Fatalf("ActiveACMEStates = %+v, %v", states, err)
	}
	s := states[0]
	if s.AccountID != a.ID || s.DomainID != a.DomainID || s.Domain != "a.com" || s.Subdomain != a.Subdomain || s.Username != a.Username ||
		s.CNAMEStatus != cnamecheck.StatusUnknown || s.ConsecutiveFailures != 0 || s.LastUpdateAt != nil || s.CNAMECheckedAt != nil ||
		!slices.Equal(s.AllowFrom, []netip.Prefix{allow}) {
		t.Fatalf("state = %+v", s)
	}

	for range 2 {
		if err := st.RecordUpdateResult(ctx, a.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	s, err = st.ACMEStateByDomain(ctx, a.DomainID)
	if err != nil || s.ConsecutiveFailures != 2 || s.LastFailureAt == nil {
		t.Fatalf("after failures = %+v, %v", s, err)
	}
	if err := st.RecordUpdateResult(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	s, _ = st.ACMEStateByDomain(ctx, a.DomainID)
	if s.ConsecutiveFailures != 0 || s.LastUpdateAt == nil {
		t.Fatalf("after success = %+v", s)
	}

	if err := st.SetCNAMEStatus(ctx, a.ID, cnamecheck.StatusMismatch, "x.other.test", t0); err != nil {
		t.Fatal(err)
	}
	s, _ = st.ACMEStateByDomain(ctx, a.DomainID)
	if s.CNAMEStatus != cnamecheck.StatusMismatch || s.CNAMEValue != "x.other.test" || s.CNAMECheckedAt == nil || !s.CNAMECheckedAt.Equal(t0) {
		t.Fatalf("after cname = %+v", s)
	}

	// 조회 오류는 마지막으로 알아낸 값을 지우지 않는다.
	if err := st.SetCNAMEStatus(ctx, a.ID, cnamecheck.StatusError, "", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s, _ = st.ACMEStateByDomain(ctx, a.DomainID)
	if s.CNAMEStatus != cnamecheck.StatusError || s.CNAMEValue != "x.other.test" || !s.CNAMECheckedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("after cname error = %+v", s)
	}

	b := mustDomain(t, st, "b.com")
	if _, err := st.ACMEStateByDomain(ctx, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no account = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if states, _ := st.ActiveACMEStates(ctx); len(states) != 0 {
		t.Fatalf("revoked account listed: %+v", states)
	}
}
