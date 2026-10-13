// Package app assembles the bot from its parts. Both ways of running it use
// this: the long-polling command (cmd/bot) and the webhook function (api).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/service"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
	"telegram-ai-bot/internal/webhook"
)

// queuePerWorker is how many updates may wait for one worker before new ones
// are refused with a "busy" reply.
const queuePerWorker = 8

// Bot is the Telegram client together with the account it acts as.
type Bot struct {
	Client *telegram.Client
	User   telegram.User
}

// Connect creates the Telegram client and asks Telegram who the bot is.
// Mentions and replies in groups are matched against that ID and username,
// and a wrong token fails here instead of on the first message.
func Connect(ctx context.Context, cfg config.Config, logger *slog.Logger) (Bot, error) {
	client := telegram.NewClient(telegram.Options{
		Token:   cfg.TelegramToken,
		BaseURL: cfg.TelegramAPIURL,
		Logger:  logger,
	})
	me, err := client.GetMe(ctx)
	if err != nil {
		return Bot{}, fmt.Errorf("identify the bot: %w", err)
	}
	return Bot{Client: client, User: me}, nil
}

// Mode says how the service will be driven.
type Mode struct {
	// Serverless is true when each update is handled inside one short-lived
	// request (a webhook function). Nothing can then be kept in memory from
	// one update to the next: there are no workers, the recent-chatter
	// buffer is off, and mute state is read from the database when needed.
	Serverless bool
	// MutedChats are the groups muted before this start. Unused when
	// Serverless.
	MutedChats []int64
}

// groupMembers answers the service's administrator check through Telegram.
type groupMembers struct {
	client *telegram.Client
}

func (m groupMembers) IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	member, err := m.client.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	return member.IsAdmin(), nil
}

// NewService wires the tools, the model client, the agent and the service.
func NewService(cfg config.Config, store *storage.Store, bot Bot, mode Mode, logger *slog.Logger) (*service.Service, error) {
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
		Store:        store,
		Model:        model,
		Tools:        registry,
		Persona:      cfg.PersonaPrompt,
		GroupNote:    cfg.GroupNote,
		MaxHistory:   cfg.MaxHistoryMessages,
		MaxToolCalls: cfg.MaxToolCallsPerTurn,
	})
	opts := service.Options{
		Sender:        bot.Client,
		Replier:       assistant,
		Resetter:      store,
		Recorder:      store,
		Members:       groupMembers{client: bot.Client},
		Muter:         store,
		Logger:        logger,
		Model:         cfg.Model,
		Workers:       cfg.MaxConcurrentRequests,
		QueueSize:     queuePerWorker,
		RatePerMinute: cfg.UserRateLimitPerMinute,
		MaxInputChars: cfg.MaxInputChars,
		TurnTimeout:   cfg.RequestTimeout,
		IsAdmin:       cfg.IsAdmin,
		AdminCommands: cfg.AdminCommands,

		Bot:                service.Identity{ID: bot.User.ID, Username: bot.User.Username},
		MentionTrigger:     cfg.MentionTrigger,
		ReplyTrigger:       cfg.ReplyTrigger,
		WakeWordTrigger:    cfg.WakeWordTrigger,
		WakeWords:          cfg.WakeWords,
		DisplayName:        cfg.BotDisplayName,
		AmbientMessages:    cfg.AmbientMessages,
		MutedChats:         mode.MutedChats,
		GroupRatePerMinute: cfg.GroupRateLimitPerMinute,
		MaxGroupReplyChars: cfg.MaxGroupReplyChars,
	}
	if mode.Serverless {
		opts.Workers, opts.QueueSize = 0, 0
		opts.AmbientMessages = 0
		opts.MutedChats = nil
		opts.MuteStore = store
	}
	return service.New(opts), nil
}

// Webhook is a running webhook receiver.
type Webhook struct {
	Handler http.Handler
	// Bot is the account updates are handled as.
	Bot Bot
	// Close releases the database pool. A serverless function never calls
	// it; tests and other hosts do.
	Close func()
}

// NewWebhook builds everything needed to handle updates delivered by
// webhook: it connects to the database, checks the schema, identifies the
// bot, and returns the HTTP handler. Each update is fully handled, reply
// included, before the handler responds.
func NewWebhook(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Webhook, error) {
	if cfg.WebhookSecret == "" {
		return nil, errors.New("TELEGRAM_WEBHOOK_SECRET is required to receive updates by webhook")
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	hook, err := newWebhook(ctx, cfg, store, logger)
	if err != nil {
		store.Close()
		return nil, err
	}
	return hook, nil
}

func newWebhook(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) (*Webhook, error) {
	if err := store.CheckSchema(ctx); err != nil {
		return nil, err
	}
	bot, err := Connect(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	svc, err := NewService(cfg, store, bot, Mode{Serverless: true}, logger)
	if err != nil {
		return nil, err
	}
	handler, err := webhook.NewHandler(webhook.Options{Secret: cfg.WebhookSecret, Handle: svc.HandleNow, Logger: logger})
	if err != nil {
		return nil, err
	}
	return &Webhook{
		Handler: handler,
		Bot:     bot,
		Close: func() {
			svc.Close()
			store.Close()
		},
	}, nil
}
