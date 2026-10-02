package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ssl-checker/internal/app"
)

const usage = `usage: ssl-checker <command> [flags]

commands:
  migrate                    DB 마이그레이션을 적용하고 종료한다
  serve [--listen :8080]     acme-dns 호환 API를 연다
  healthcheck [--url URL]    실행 중인 api의 /health를 확인한다
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "migrate":
		err = app.Migrate(ctx, os.Getenv)
	case "serve":
		err = app.Serve(ctx, os.Args[2:], os.Getenv)
	case "healthcheck":
		err = app.Healthcheck(ctx, os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}
