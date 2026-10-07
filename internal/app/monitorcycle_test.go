package app

import (
	"context"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMonitorCycleChecksTargetsAndCNAMEs(t *testing.T) {
	st := store.New(dbtest.New(t))
	ctx := context.Background()

	// httptest 인증서는 example.com과 127.0.0.1을 덮는 자체 서명 인증서다. 그 자체를 루트로 믿게 한다.
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	sub := uuid.New()
	acct, err := st.CreateAccount(ctx, store.NewAccount{Domain: "a.com", Username: uuid.New(), KeyHash: "h", Subdomain: sub})
	if err != nil {
		t.Fatal(err)
	}
	tg, err := st.CreateTarget(ctx, store.NewTarget{DomainID: acct.DomainID, Hostname: "example.com", Port: port,
		IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	resolver := cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.a.com.": {CNAME: sub.String() + ".acme.test."},
	})

	c := &monitorCycle{
		Store:  st,
		Runner: &monitor.Runner{Prober: &monitor.Prober{Roots: roots}, Sink: st, Log: quiet},
		CNAME:  &cnamecheck.Checker{Resolver: resolver},
		Zone:   "acme.test",
		Log:    quiet,
	}
	c.run(ctx)

	latest, err := st.LatestResults(ctx, []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	if rs := latest[tg.ID]; len(rs) != 1 || !rs[0].ChainOK || !rs[0].HostnameOK || rs[0].Error != "" {
		t.Fatalf("results = %+v", rs)
	}
	s, err := st.ACMEStateByDomain(ctx, acct.DomainID)
	if err != nil || s.CNAMEStatus != cnamecheck.StatusOK || s.CNAMECheckedAt == nil {
		t.Fatalf("acme state = %+v, %v", s, err)
	}
}

func TestMonitorCyclePrune(t *testing.T) {
	st := store.New(dbtest.New(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	d, err := st.CreateDomain(ctx, "a.com", "")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := st.CreateTarget(ctx, store.NewTarget{DomainID: d.ID, Hostname: "a.com", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{now.Add(-91 * 24 * time.Hour), now.Add(-time.Hour)} {
		if err := st.SaveResults(ctx, tg.ID, []monitor.Result{{Address: "192.0.2.1:443", CheckedAt: at, Status: monitor.StatusOK}}); err != nil {
			t.Fatal(err)
		}
	}
	c := &monitorCycle{Store: st, Now: func() time.Time { return now }, Log: quiet}
	c.prune(ctx)
	hist, err := st.ResultHistory(ctx, tg.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history after prune = %+v, %v", hist, err)
	}
}
