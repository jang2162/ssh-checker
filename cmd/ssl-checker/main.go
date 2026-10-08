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
  migrate                    apply database migrations and exit
  serve [--listen :8080]     serve the acme-dns compatible API, the /api API and the dashboard
  worker                     run the periodic jobs (TLS checks, CNAME checks, TXT and history cleanup)
  healthcheck [--url URL]    check /health of a running api
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
	case "worker":
		err = app.Worker(ctx, os.Getenv)
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
