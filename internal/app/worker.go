package app

import (
	"context"
	"log/slog"
	"time"

	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/store"
)

const cleanupEvery = 24 * time.Hour

// Worker는 주기 작업을 돌린다. 1단계에서는 하루 한 번 고아 TXT 정리만 한다(시작 직후 1회 포함).
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
	cleaner := &cleanup.Cleaner{Store: store.New(pool), Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL, Log: slog.Default()}

	run := func() {
		res, err := cleaner.Run(ctx)
		if err != nil {
			slog.Error("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired, "err", err)
			return
		}
		slog.Info("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired)
	}
	run()
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}
