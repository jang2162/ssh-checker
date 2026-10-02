package app

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"time"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/store"
)

const (
	updateEvery = 6 * time.Second // 계정별 /update 토큰 보충 주기
	updateBurst = 10
)

// Serve는 acme-dns 호환 API를 연다. SIGTERM을 받으면 진행 중인 요청을 마치고 끝낸다.
func Serve(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(config.RoleAPI, getenv)
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

	ip := httpx.ClientIP{Trusted: cfg.TrustedProxies}
	h := &acmedns.Handler{
		Svc:         &acmedns.Service{Store: store.New(pool), Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL},
		IP:          ip,
		Admin:       httpx.NewAdminAuth(cfg.AdminKey, ip),
		UpdateLimit: httpx.NewKeyed(updateEvery, updateBurst),
		Health:      &acmedns.HealthChecker{DB: pool, Provider: provider, CacheFor: time.Minute},
		Log:         slog.Default(),
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("serving", "addr", *listen, "provider", cfg.DNSProvider, "zone", cfg.ChallengeZone)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
