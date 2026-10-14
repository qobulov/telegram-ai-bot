package handler

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"telegram-ai-bot/internal/app"
	"telegram-ai-bot/internal/config"
)

// cleanupSetupTimeout is defined here rather than shared with webhook.go:
// Vercel builds each file in api/ as a separate function, so each file
// keeps to itself.
const cleanupSetupTimeout = 20 * time.Second

var (
	cleanupMu sync.Mutex
	cleanup   *app.Cleanup
)

// Cleanup is the function Vercel Cron calls once a day (see vercel.json) to
// delete chat history older than RETENTION_DAYS. Vercel serves it at
// /api/cleanup and authenticates its calls with CRON_SECRET.
func Cleanup(w http.ResponseWriter, r *http.Request) {
	job, err := cleanupReady()
	if err != nil {
		slog.Error("the cleanup endpoint is not ready", "error", err)
		http.Error(w, "the cleanup endpoint is not ready; see the function logs", http.StatusServiceUnavailable)
		return
	}
	job.Handler.ServeHTTP(w, r)
}

func cleanupReady() (*app.Cleanup, error) {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	if cleanup != nil {
		return cleanup, nil
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, cancel := context.WithTimeout(context.Background(), cleanupSetupTimeout)
	defer cancel()
	job, err := app.NewCleanup(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	cleanup = job
	return cleanup, nil
}
