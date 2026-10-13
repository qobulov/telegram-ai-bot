// Command bot runs the Telegram AI assistant.
//
// Usage:
//
//	bot                     start the bot with long polling
//	bot migrate             apply database migrations and exit
//	bot set-webhook <url>   make Telegram deliver updates to a deployed webhook
//	bot delete-webhook      remove the webhook, so long polling works again
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // timezone data for get_current_time on hosts without it

	"telegram-ai-bot/internal/app"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
)

const (
	healthCheckInterval = time.Minute
	retentionInterval   = time.Hour
	dayDuration         = 24 * time.Hour
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case len(args) == 0:
		return runBot(ctx)
	case len(args) == 1 && args[0] == "migrate":
		return runMigrate(ctx)
	case len(args) == 2 && args[0] == "set-webhook":
		return runSetWebhook(ctx, args[1])
	case len(args) == 1 && args[0] == "delete-webhook":
		return runDeleteWebhook(ctx)
	default:
		return fmt.Errorf("unknown arguments %q; see the usage at the top of cmd/bot/main.go or in README.md", args)
	}
}

func runBot(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	return serve(ctx, cfg, store, newLogger(cfg.LogLevel))
}

// runMigrate needs only DB_POSTGRES_URL. Migrations are applied by an operator
// with the database owner role, who should not have to supply the bot's
// Telegram and OpenRouter secrets to do it.
func runMigrate(ctx context.Context) error {
	databaseURL := strings.TrimSpace(os.Getenv("DB_POSTGRES_URL"))
	if databaseURL == "" {
		return errors.New("DB_POSTGRES_URL is required")
	}
	store, err := storage.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	applied, err := store.Migrate(ctx)
	if err != nil {
		return err
	}
	newLogger(slog.LevelInfo).Info("migrations finished", "applied", applied)
	return nil
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func serve(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) error {
	if err := store.CheckSchema(ctx); err != nil {
		return err
	}
	bot, err := app.Connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	mutedChats, err := store.MutedChats(ctx)
	if err != nil {
		return err
	}
	svc, err := app.NewService(cfg, store, bot, app.Mode{MutedChats: mutedChats}, logger)
	if err != nil {
		return err
	}

	// The maintenance loop stops when polling does, whatever the reason.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var background sync.WaitGroup
	background.Go(func() { maintain(ctx, cfg, store, logger) })

	logger.Info("bot started", "username", bot.User.Username, "name", cfg.BotDisplayName,
		"model", cfg.Model, "workers", cfg.MaxConcurrentRequests, "muted_groups", len(mutedChats))
	pollErr := bot.Client.Poll(ctx, svc.Handle)

	// Polling has stopped, so no new updates arrive. Let queued and running
	// turns finish before the caller closes the database pool.
	logger.Info("shutting down; waiting for messages in progress")
	svc.Close()
	cancel()
	background.Wait()
	logger.Info("bot stopped")
	return pollErr
}

// runSetWebhook points Telegram at a deployed webhook function. After this,
// long polling with the same token stops working until delete-webhook.
func runSetWebhook(ctx context.Context, url string) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.WebhookSecret == "" {
		return errors.New("TELEGRAM_WEBHOOK_SECRET is required; it must be the same value the deployed function has")
	}
	if !strings.HasPrefix(url, "https://") {
		return errors.New("the webhook URL must start with https://")
	}
	logger := newLogger(cfg.LogLevel)
	bot, err := app.Connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if err := bot.Client.SetWebhook(ctx, url, cfg.WebhookSecret); err != nil {
		return err
	}
	logger.Info("webhook set", "username", bot.User.Username, "url", url)
	return nil
}

func runDeleteWebhook(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	bot, err := app.Connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if err := bot.Client.DeleteWebhook(ctx); err != nil {
		return err
	}
	logger.Info("webhook deleted; long polling can be used again", "username", bot.User.Username)
	return nil
}

// maintain runs until ctx is cancelled: it checks that the database is
// reachable every minute and applies the retention policy every hour.
func maintain(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) {
	health := time.NewTicker(healthCheckInterval)
	defer health.Stop()
	retention := time.NewTicker(retentionInterval)
	defer retention.Stop()

	applyRetention(ctx, cfg, store, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-health.C:
			if err := store.Ping(ctx); err != nil && ctx.Err() == nil {
				logger.Error("database health check failed", "error", err)
			}
		case <-retention.C:
			applyRetention(ctx, cfg, store, logger)
		}
	}
}

func applyRetention(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) {
	if cfg.RetentionDays == 0 {
		return
	}
	deleted, err := store.DeleteIdle(ctx, time.Duration(cfg.RetentionDays)*dayDuration)
	if err != nil {
		if ctx.Err() == nil {
			logger.Error("retention cleanup failed", "error", err)
		}
		return
	}
	if deleted > 0 {
		logger.Info("retention cleanup removed idle conversations", "count", deleted, "older_than_days", cfg.RetentionDays)
	}
}
