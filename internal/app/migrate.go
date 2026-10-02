// Package app는 하위 명령(migrate, serve, worker, healthcheck)의 실행 흐름을 담는다.
package app

import (
	"context"

	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
)

// Migrate는 DB 마이그레이션을 적용하고 끝난다.
func Migrate(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(config.RoleMigrate, getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return db.Migrate(ctx, pool)
}
