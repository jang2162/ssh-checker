package app

import (
	"context"
	"log/slog"
	"time"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

// resultRetention보다 오래된 점검 기록은 하루 한 번 지운다.
const resultRetention = 90 * 24 * time.Hour

// monitorCycle은 worker가 CHECK_INTERVAL마다 돌리는 작업이다.
type monitorCycle struct {
	Store  *store.Store
	Runner *monitor.Runner
	CNAME  *cnamecheck.Checker
	Zone   string
	Now    func() time.Time
	Log    *slog.Logger
}

func (c *monitorCycle) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// run은 모든 대상을 TLS 점검하고, ACME가 켜진 도메인의 CNAME을 확인한다. 한쪽이 실패해도 다른 쪽은 돈다.
func (c *monitorCycle) run(ctx context.Context) {
	targets, err := c.Store.ListTargets(ctx)
	if err != nil {
		c.Log.Error("tls check: list targets", "err", err)
	} else {
		ts := make([]monitor.Target, len(targets))
		for i, t := range targets {
			ts[i] = t.Target
		}
		failed := c.Runner.CheckAll(ctx, ts)
		c.Log.Info("tls check", "targets", len(ts), "save_failed", failed)
	}

	states, err := c.Store.ActiveACMEStates(ctx)
	if err != nil {
		c.Log.Error("cname check: list accounts", "err", err)
		return
	}
	for _, s := range states {
		res := c.CNAME.Check(ctx, s.Domain, s.Subdomain+"."+c.Zone)
		if res.Err != nil {
			c.Log.Warn("cname check", "domain", s.Domain, "err", res.Err)
		}
		if err := c.Store.SetCNAMEStatus(ctx, s.AccountID, res.Status, res.Value, c.now()); err != nil {
			c.Log.Error("cname check: save", "domain", s.Domain, "err", err)
		}
	}
	c.Log.Info("cname check", "domains", len(states))
}

func (c *monitorCycle) prune(ctx context.Context) {
	n, err := c.Store.PruneResults(ctx, c.now().Add(-resultRetention))
	if err != nil {
		c.Log.Error("prune check results", "err", err)
		return
	}
	c.Log.Info("prune check results", "deleted", n)
}
