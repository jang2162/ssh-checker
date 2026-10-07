package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

const cleanupEvery = 24 * time.Hour

// Worker는 주기 작업을 돌린다(시작 직후 1회 포함).
//   - CHECK_INTERVAL마다: 모든 대상의 TLS 점검, ACME 도메인의 CNAME 확인
//   - 하루 한 번: 고아 TXT 정리, 90일 지난 점검 기록 삭제
//
// 작업 중복을 막기 위해 worker는 1개만 띄운다.
func Worker(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(config.RoleWorker, getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := newProvider(cfg)
	if err != nil {
		return err
	}
	st := store.New(pool)
	log := slog.Default()
	cleaner := &cleanup.Cleaner{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL, Log: log}
	cycle := &monitorCycle{
		Store:  st,
		Runner: &monitor.Runner{Prober: &monitor.Prober{}, Sink: st, Log: log},
		CNAME:  &cnamecheck.Checker{Resolver: cfg.DNSResolver},
		Zone:   cfg.ChallengeZone,
		Log:    log,
	}

	daily := func() {
		res, err := cleaner.Run(ctx)
		if err != nil {
			log.Error("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired, "err", err)
		} else {
			log.Info("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired)
		}
		cycle.prune(ctx)
	}
	var wg sync.WaitGroup
	wg.Go(func() { every(ctx, cleanupEvery, daily) })
	wg.Go(func() { every(ctx, cfg.CheckInterval, func() { cycle.run(ctx) }) })
	wg.Wait()
	return nil
}

// every는 fn을 바로 한 번 실행하고, 이후 interval마다 실행한다. ctx가 끝나면 돌아온다.
// 한 번의 실행이 interval보다 길면 밀린 틱은 하나로 합쳐진다(time.Ticker 동작).
func every(ctx context.Context, interval time.Duration, fn func()) {
	fn()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}
