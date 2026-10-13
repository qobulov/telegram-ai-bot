// Command bot runs the Telegram AI assistant.
//
// Usage:
//
//	bot            start the bot
//	bot migrate    apply database migrations and exit
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // timezone data for get_current_time on hosts without it

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/service"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

const (
	// queuePerWorker is how many updates may wait for one worker before new
	// ones are refused with a "busy" reply.
	queuePerWorker      = 8
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
	default:
		return fmt.Errorf("unknown arguments %q; run without arguments to start the bot, or with \"migrate\"", args)
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

// runMigrate needs only DATABASE_URL. Migrations are applied by an operator
// with the database owner role, who should not have to supply the bot's
// Telegram and OpenRouter secrets to do it.
func runMigrate(ctx context.Context) error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
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
	svc, err := newService(cfg, store, logger)
	if err != nil {
		return err
	}
	bot := telegram.NewClient(telegram.Options{
		Token:   cfg.TelegramToken,
		BaseURL: cfg.TelegramAPIURL,
		Logger:  logger,
	})

	// The maintenance loop stops when polling does, whatever the reason.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var background sync.WaitGroup
	background.Go(func() { maintain(ctx, cfg, store, logger) })

	logger.Info("bot started", "model", cfg.Model, "workers", cfg.MaxConcurrentRequests)
	pollErr := bot.Poll(ctx, svc.Handle)

	// Polling has stopped, so no new updates arrive. Let queued and running
	// turns finish before the caller closes the database pool.
	logger.Info("shutting down; waiting for messages in progress")
	svc.Close()
	cancel()
	background.Wait()
	logger.Info("bot stopped")
	return pollErr
}

func newService(cfg config.Config, store *storage.Store, logger *slog.Logger) (*service.Service, error) {
	registry, err := tools.NewRegistry(cfg.IsAdmin, tools.Builtin(service.HelpText())...)
	if err != nil {
		return nil, err
	}
	model := ai.NewClient(ai.Options{
		BaseURL:    cfg.OpenRouterBaseURL,
		APIKey:     cfg.OpenRouterAPIKey,
		Model:      cfg.Model,
		MaxTokens:  cfg.MaxOutputTokens,
		HTTPClient: &http.Client{Timeout: cfg.RequestTimeout},
		Logger:     logger,
	})
	assistant := agent.New(agent.Options{
		Store:             store,
		Model:             model,
		Tools:             registry,
		SystemPrompt:      cfg.SystemPrompt,
		GroupSystemPrompt: cfg.GroupSystemPrompt,
		MaxHistory:        cfg.MaxHistoryMessages,
		MaxToolCalls:      cfg.MaxToolCallsPerTurn,
	})
	sender := telegram.NewClient(telegram.Options{
		Token:   cfg.TelegramToken,
		BaseURL: cfg.TelegramAPIURL,
		Logger:  logger,
	})
	return service.New(service.Options{
		Sender:        sender,
		Replier:       assistant,
		Resetter:      store,
		Logger:        logger,
		Model:         cfg.Model,
		Workers:       cfg.MaxConcurrentRequests,
		QueueSize:     queuePerWorker,
		RatePerMinute: cfg.UserRateLimitPerMinute,
		MaxInputChars: cfg.MaxInputChars,
		TurnTimeout:   cfg.RequestTimeout,
		IsAdmin:       cfg.IsAdmin,
		AdminCommands: cfg.AdminCommands,
	}), nil
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
