package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func mustDomain(t *testing.T, st *store.Store, fqdn string) store.Domain {
	t.Helper()
	d, err := st.CreateDomain(context.Background(), fqdn, "")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	return d
}

func mustTarget(t *testing.T, st *store.Store, domainID int64, host string, ips ...string) store.Target {
	t.Helper()
	in := store.NewTarget{DomainID: domainID, Hostname: host, Port: 443}
	for _, ip := range ips {
		in.IPs = append(in.IPs, netip.MustParseAddr(ip))
	}
	tg, err := st.CreateTarget(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	return tg
}

func okResult(addr string, at time.Time, serial string) monitor.Result {
	return monitor.Result{
		Address: addr, CheckedAt: at, Status: monitor.StatusOK, Reasons: []string{},
		TLSVersion: "TLS 1.3", Cipher: "TLS_AES_128_GCM_SHA256", SubjectCN: "a.com", SANs: []string{"a.com"},
		Issuer: "R11", Serial: serial, NotBefore: at.Add(-24 * time.Hour), NotAfter: at.Add(60 * 24 * time.Hour),
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true,
	}
}

func save(t *testing.T, st *store.Store, targetID int64, rs ...monitor.Result) {
	t.Helper()
	if err := st.SaveResults(context.Background(), targetID, rs); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
}

func TestDomains(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	b := mustDomain(t, st, "b.com")
	a, err := st.CreateDomain(ctx, "a.com", "memo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDomain(ctx, "a.com", ""); !errors.Is(err, store.ErrDomainExists) {
		t.Fatalf("duplicate = %v", err)
	}
	list, err := st.ListDomains(ctx)
	if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID || list[0].Memo != "memo" {
		t.Fatalf("ListDomains = %+v, %v", list, err)
	}
	got, err := st.GetDomain(ctx, a.ID)
	if err != nil || got.FQDN != "a.com" || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("GetDomain = %+v, %v", got, err)
	}
	if _, err := st.GetDomain(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestTargets(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	d := mustDomain(t, st, "a.com")
	in := store.NewTarget{DomainID: d.ID, Hostname: "www.a.com", Port: 8443, SNI: "a.com",
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("2001:db8::1")}, Memo: "web"}
	tg, err := st.CreateTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTarget(ctx, tg.ID)
	if err != nil || got.Hostname != "www.a.com" || got.Port != 8443 || got.SNI != "a.com" || got.DomainID != d.ID ||
		got.Memo != "web" || !slices.Equal(got.IPs, in.IPs) {
		t.Fatalf("GetTarget = %+v, %v", got, err)
	}
	if addrs := got.DialAddresses(); !slices.Equal(addrs, []string{"10.0.0.1:8443", "[2001:db8::1]:8443"}) {
		t.Fatalf("DialAddresses = %v", addrs)
	}
	if _, err := st.CreateTarget(ctx, in); !errors.Is(err, store.ErrTargetExists) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := st.CreateTarget(ctx, store.NewTarget{DomainID: 99999, Hostname: "x", Port: 443}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown domain = %v", err)
	}
	other := mustDomain(t, st, "b.com")
	mustTarget(t, st, other.ID, "b.com")
	byDomain, err := st.TargetsByDomain(ctx, d.ID)
	if err != nil || len(byDomain) != 1 || byDomain[0].ID != tg.ID {
		t.Fatalf("TargetsByDomain = %+v, %v", byDomain, err)
	}
	if all, err := st.ListTargets(ctx); err != nil || len(all) != 2 {
		t.Fatalf("ListTargets = %+v, %v", all, err)
	}
	if _, err := st.GetTarget(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestResultsLatestAndHistory(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1")
	first := okResult("10.0.0.1:443", t0.Add(-2*time.Hour), "01")
	failed := monitor.Result{Address: "10.0.0.1:443", CheckedAt: t0.Add(-time.Hour), Status: monitor.StatusCritical,
		Reasons: []string{monitor.CodeConnectFailed}, Error: "connection refused"}
	save(t, st, tg.ID, first)
	save(t, st, tg.ID, failed)

	latest, err := st.LatestResults(ctx, []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	l := latest[tg.ID]
	if len(l) != 1 || l[0].Error != "connection refused" || l[0].HasCert() || l[0].Status != monitor.StatusCritical ||
		!slices.Equal(l[0].Reasons, []string{monitor.CodeConnectFailed}) || l[0].SANs == nil || len(l[0].SANs) != 0 {
		t.Fatalf("latest = %+v", l)
	}

	hist, err := st.ResultHistory(ctx, tg.ID, 10)
	if err != nil || len(hist) != 2 || hist[0].Status != monitor.StatusCritical {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	h := hist[1]
	if h.Serial != "01" || !h.NotAfter.Equal(first.NotAfter) || !h.CheckedAt.Equal(first.CheckedAt) || h.CheckedAt.Location() != time.UTC ||
		!h.ChainOK || h.KeyBits != 256 || !slices.Equal(h.SANs, []string{"a.com"}) || h.Cipher != first.Cipher {
		t.Fatalf("history[1] = %+v", h)
	}
	if hist, _ := st.ResultHistory(ctx, tg.ID, 1); len(hist) != 1 {
		t.Fatalf("limit ignored: %d", len(hist))
	}
}

// IP 목록에서 뺀 주소의 옛 결과는 최신 결과에 섞이면 안 된다.
func TestLatestResultsDropsRemovedIPs(t *testing.T) {
	st, _ := newStore(t)
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1", "10.0.0.2")
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0, "01"), okResult("10.0.0.2:443", t0, "02"))
	tg.IPs = tg.IPs[:1] // 설정에서 10.0.0.2를 뺐다
	latest, err := st.LatestResults(context.Background(), []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	if l := latest[tg.ID]; len(l) != 1 || l[0].Address != "10.0.0.1:443" {
		t.Fatalf("latest = %+v", l)
	}
}

func TestLatestResultsHostnameMode(t *testing.T) {
	st, _ := newStore(t)
	d := mustDomain(t, st, "a.com")
	tg := mustTarget(t, st, d.ID, "www.a.com")
	empty := mustTarget(t, st, d.ID, "api.a.com")
	save(t, st, tg.ID, okResult("192.0.2.1:443", t0.Add(-time.Hour), "01"))
	save(t, st, tg.ID, okResult("192.0.2.2:443", t0, "02"))
	latest, err := st.LatestResults(context.Background(), []store.Target{tg, empty})
	if err != nil {
		t.Fatal(err)
	}
	if l := latest[tg.ID]; len(l) != 1 || l[0].Serial != "02" {
		t.Fatalf("latest = %+v", l)
	}
	if l, ok := latest[empty.ID]; !ok || l == nil || len(l) != 0 {
		t.Fatalf("unchecked target = %v, %v", l, ok)
	}
}

func TestPruneResults(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1")
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0.Add(-91*24*time.Hour), "01"))
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0, "02"))
	n, err := st.PruneResults(ctx, t0.Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("PruneResults = %d, %v", n, err)
	}
	if hist, _ := st.ResultHistory(ctx, tg.ID, 10); len(hist) != 1 || hist[0].Serial != "02" {
		t.Fatalf("history = %+v", hist)
	}
}
