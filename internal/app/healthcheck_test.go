package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ssl-checker/internal/app"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"db":"ok","provider":"config"}`))
	}))
	defer srv.Close()

	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err != nil {
		t.Fatalf("healthy: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err == nil {
		t.Fatal("503 reported healthy")
	}
	srv.Close()
	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err == nil {
		t.Fatal("connection refused reported healthy")
	}
}
