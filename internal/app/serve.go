package app

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"time"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/adminapi"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

const (
	updateEvery = 6 * time.Second // 계정별 /update 토큰 보충 주기
	updateBurst = 10

	// /update 인증 실패는 IP별로 20번까지 받고, 그 뒤로는 3초에 1번만 받는다.
	// 관리자 인증보다 느슨한 이유: NAT 뒤 여러 서버가 IP 하나를 함께 쓸 수 있고,
	// compat 스위트도 한 IP에서 인증 실패를 7번 보낸다.
	authFailEvery = 3 * time.Second
	authFailBurst = 20
)

// Serve는 acme-dns 호환 API와 /api(조회·관리)를 연다. SIGTERM을 받으면 진행 중인 요청을 마치고 끝낸다.
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
	st := store.New(pool)
	svc := &acmedns.Service{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL}
	h := &acmedns.Handler{
		Svc:           svc,
		IP:            ip,
		Admin:         httpx.NewAdminAuth(cfg.AdminKey, ip),
		UpdateLimit:   httpx.NewKeyed(updateEvery, updateBurst),
		AuthFailLimit: httpx.NewKeyed(authFailEvery, authFailBurst),
		Health:        &acmedns.HealthChecker{DB: pool, Provider: provider, CacheFor: time.Minute},
		Log:           slog.Default(),
	}
	api := &adminapi.API{
		Store: st, Svc: svc, Zone: cfg.ChallengeZone, CheckInterval: cfg.CheckInterval, PublicURL: cfg.PublicURL, IP: ip,
		Runner: &monitor.Runner{Prober: &monitor.Prober{}, Sink: st, Log: slog.Default()},
		CNAME:  &cnamecheck.Checker{Resolver: cfg.DNSResolver},
		Auth:   adminapi.NewAuth(cfg.AdminKey),
		Log:    slog.Default(),
	}

	// acme-dns 경로는 기존 핸들러에 그대로 넘기고, /api는 같은 mux에 붙인다.
	mux := http.NewServeMux()
	legacy := h.Routes()
	mux.Handle("POST /update", legacy)
	mux.Handle("POST /register", legacy)
	mux.Handle("GET /health", legacy)
	api.Register(mux)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
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
