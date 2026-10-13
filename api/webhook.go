// Package handler is the Vercel serverless function that receives Telegram
// updates by webhook. Vercel serves it at /api/webhook.
//
// The long-polling command in cmd/bot is the other way to run the same bot;
// a bot token can use only one of the two at a time.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
	_ "time/tzdata" // timezone data for get_current_time; the host has none

	"telegram-ai-bot/internal/app"
	"telegram-ai-bot/internal/config"
)

const setupTimeout = 20 * time.Second

var (
	mu      sync.Mutex
	webhook *app.Webhook
)

// Handler handles one request. The first request of a new instance connects
// to the database and to Telegram; later ones reuse that.
func Handler(w http.ResponseWriter, r *http.Request) {
	hook, err := ready()
	if err != nil {
		// The message names missing settings but never their values.
		slog.Error("the webhook is not ready", "error", err)
		// 503 makes Telegram deliver the update again later.
		http.Error(w, "the bot is not ready; see the function logs", http.StatusServiceUnavailable)
		return
	}
	hook.Handler.ServeHTTP(w, r)
}

// ready returns the webhook, setting it up on first use. A failed setup is
// tried again on the next request rather than remembered, so a database that
// was briefly unreachable does not break the instance for good.
func ready() (*app.Webhook, error) {
	mu.Lock()
	defer mu.Unlock()
	if webhook != nil {
		return webhook, nil
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	hook, err := app.NewWebhook(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("webhook ready", "username", hook.Bot.User.Username, "name", cfg.BotDisplayName, "model", cfg.Model)
	webhook = hook
	return webhook, nil
}
