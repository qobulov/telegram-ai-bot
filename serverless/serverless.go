// Package serverless holds the Vercel functions' handlers.
//
// The files in api/ are compiled by Vercel as a separate module, so they may
// not import this module's internal packages. They call this package, which
// is outside internal/ and may.
package serverless

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

const setupTimeout = 20 * time.Second

// lazy builds a handler on first use and keeps it for later requests of the
// same instance. A failed setup is tried again on the next request rather
// than remembered, so a briefly unreachable database does not break the
// instance for good.
type lazy struct {
	name    string
	build   func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, error)
	mu      sync.Mutex
	handler http.Handler
}

func (l *lazy) serve(w http.ResponseWriter, r *http.Request) {
	handler, err := l.ready()
	if err != nil {
		// The error names missing settings, never their values.
		slog.Error("function is not ready", "function", l.name, "error", err)
		// 503 makes Telegram deliver the update again later.
		http.Error(w, l.name+" is not ready; see the function logs", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}

func (l *lazy) ready() (http.Handler, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.handler != nil {
		return l.handler, nil
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	handler, err := l.build(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	l.handler = handler
	return handler, nil
}

var webhook = &lazy{name: "webhook", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	hook, err := app.NewWebhook(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("webhook ready", "username", hook.Bot.User.Username, "name", cfg.BotDisplayName, "model", cfg.Model)
	return hook.Handler, nil
}}

var cleanup = &lazy{name: "cleanup", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	job, err := app.NewCleanup(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	return job.Handler, nil
}}

// Webhook receives Telegram updates.
func Webhook(w http.ResponseWriter, r *http.Request) { webhook.serve(w, r) }

// Cleanup deletes old chat history when Vercel Cron calls it.
func Cleanup(w http.ResponseWriter, r *http.Request) { cleanup.serve(w, r) }
