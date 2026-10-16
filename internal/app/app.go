// Package app assembles the bot from its parts. Both ways of running it use
// this: the long-polling command (cmd/bot) and the webhook function (api).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/cron"
	"telegram-ai-bot/internal/service"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
	"telegram-ai-bot/internal/webhook"
)

// queuePerWorker is how many updates may wait for one worker before new ones
// are refused with a "busy" reply.
const queuePerWorker = 8

// attemptTimeout bounds one request to the model provider.
const attemptTimeout = 20 * time.Second

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
	// one update to the next: there are no workers, and mute state is read
	// from the database when needed. The group log is in the database in
	// both modes.
	Serverless bool
	// MutedChats are the groups muted before this start. Unused when
	// Serverless.
	MutedChats []int64
}

// groupMembers answers the service's administrator check through Telegram.
type groupMembers struct {
	client *telegram.Client
	// botID is the bot's own user ID, for asking about its membership.
	botID int64
}

func (m groupMembers) IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	member, err := m.client.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	return member.IsAdmin(), nil
}

// Membership asks Telegram about the bot in a group, for the owners'
// bot_database tool: its status and admin rights, and for a group it is in,
// the member count and current title.
func (m groupMembers) Membership(ctx context.Context, chatID int64) (tools.Membership, error) {
	me, err := m.client.GetChatMember(ctx, chatID, m.botID)
	if err != nil {
		return tools.Membership{}, membershipError(err)
	}
	membership := tools.Membership{Status: me.Status, Rights: me.Rights()}
	if !me.IsMember() {
		return membership, nil
	}
	if membership.Members, err = m.client.GetChatMemberCount(ctx, chatID); err != nil {
		return tools.Membership{}, membershipError(err)
	}
	if chat, err := m.client.GetChat(ctx, chatID); err == nil {
		membership.Title = chat.Title
	}
	return membership, nil
}

// rightsStore keeps the bot's admin rights in each group.
type rightsStore interface {
	BotRights(ctx context.Context, chatID int64) (rights []string, known bool, err error)
	SetBotRights(ctx context.Context, chatID int64, status string, rights []string) error
}

// memberLookup asks Telegram about a member of a chat.
type memberLookup interface {
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
}

// botRights tells the service which admin rights the bot holds in a group,
// for the turn header. Telegram reports every change to them in a
// my_chat_member update, which is stored, so the stored rights are used
// when there are any. A group joined before rights were kept has none
// stored; Telegram is asked once and the answer stored.
type botRights struct {
	store   rightsStore
	members memberLookup
	botID   int64
	logger  *slog.Logger
}

func (b botRights) BotRights(ctx context.Context, chatID int64) ([]string, error) {
	rights, known, err := b.store.BotRights(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if known {
		return rights, nil
	}
	me, err := b.members.GetChatMember(ctx, chatID, b.botID)
	if err != nil {
		return nil, err
	}
	rights = me.Rights()
	// Storing only saves asking again: Telegram's answer holds either way.
	if err := b.store.SetBotRights(ctx, chatID, me.Status, rights); err != nil {
		b.logger.Warn("could not store the bot's admin rights", "chat_id", chatID, "error", err)
	}
	return rights, nil
}

// groupSender posts an owner's confirmed broadcast in a group.
type groupSender struct {
	client *telegram.Client
}

func (g groupSender) SendToGroup(ctx context.Context, chatID int64, text string) error {
	_, err := g.client.SendMessage(ctx, telegram.Outgoing{ChatID: chatID, Text: text})
	return err
}

// membershipError reports the bot having been removed, or the group having
// gone, as tools.ErrNotInGroup.
func membershipError(err error) error {
	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.Code == http.StatusForbidden || strings.Contains(apiErr.Description, "chat not found") {
		return fmt.Errorf("%w: %s", tools.ErrNotInGroup, apiErr.Description)
	}
	return err
}

// NewService wires the tools, the model client, the agent and the service.
func NewService(cfg config.Config, store *storage.Store, bot Bot, mode Mode, logger *slog.Logger) (*service.Service, error) {
	registry, err := tools.NewRegistry(cfg.IsAdmin, builtinTools(cfg, store, bot)...)
	if err != nil {
		return nil, err
	}
	model := ai.NewClient(ai.Options{
		BaseURL:   cfg.AIBaseURL,
		APIKey:    cfg.AIAPIKey,
		Model:     cfg.Model,
		MaxTokens: cfg.MaxOutputTokens,
		// Two rounds through every model; the turn's deadline still applies.
		MaxAttempts: 2 * (1 + len(cfg.FallbackModels)),
		HTTPClient:  &http.Client{Timeout: cfg.RequestTimeout},
		// A stuck attempt is retried well before the turn runs out of time.
		AttemptTimeout: attemptTimeout,
		FallbackModels: cfg.FallbackModels,
		Logger:         logger,
	})
	assistant := agent.New(agent.Options{
		Store:        store,
		Model:        withBackup(cfg, model, logger),
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
		Members:       groupMembers{client: bot.Client, botID: bot.User.ID},
		Groups:        store,
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
		ChatLog:            store,
		Location:           cfg.Location,
		OwnerUsernames:     cfg.OwnerUsernames,
		OwnerIDs:           cfg.OwnerIDs,
		AllowedBots:        cfg.AllowedBots,
		BotReplies:         store,
		BotRepliesPer10Min: cfg.BotRepliesPer10Min,
		MutedChats:         mode.MutedChats,
		MaxGroupReplyChars: cfg.MaxGroupReplyChars,
		Media:              bot.Client,
		MaxMediaBytes:      cfg.MaxMediaBytes,
		MaxVoiceSeconds:    cfg.MaxVoiceSeconds,
		AudioInput:         cfg.AudioInput,
		Transcriber:        transcriber(cfg, model),
		ImagesEnabled:      cfg.ImagesEnabled,

		GroupReplies:          store,
		GroupRatePerMinute:    cfg.GroupRateLimitPerMinute,
		MemberRepliesPer10Min: cfg.MemberRepliesPer10Min,
		Reactor:               bot.Client,
		ChatActor:             bot.Client,
		BotRights:             botRights{store: store, members: bot.Client, botID: bot.User.ID, logger: logger},
		Rules:                 store,
	}
	if mode.Serverless {
		opts.Workers, opts.QueueSize = 0, 0
		opts.MutedChats = nil
		opts.MuteStore = store
	}
	return service.New(opts), nil
}

// builtinTools are the tools the model may call in this deployment: the help
// and the image tool follow what is enabled, and the time is the groups'.
func builtinTools(cfg config.Config, store *storage.Store, bot Bot) []tools.Tool {
	help := service.HelpText(service.Abilities{Draw: cfg.ImagesEnabled, Listen: cfg.AudioInput})
	builtin := tools.Builtin(help, cfg.Location)
	if cfg.ImagesEnabled {
		builtin = append(builtin, imageTool(cfg, store))
	}
	// Any group's admins, and any member for a poll, may use these, so they
	// are there with or without owners.
	builtin = append(builtin,
		tools.ModerationTool(tools.ModerationOptions{
			BotID:    bot.User.ID,
			Members:  bot.Client,
			Telegram: bot.Client,
			Store:    store,
			Rights:   store,
			Location: cfg.Location,
		}),
		tools.PollTool(tools.PollOptions{Sender: bot.Client, Store: store}),
	)
	if len(cfg.OwnerUsernames) > 0 {
		builtin = append(builtin,
			tools.DatabaseTool(tools.DatabaseOptions{
				Store:    store,
				Members:  groupMembers{client: bot.Client, botID: bot.User.ID},
				Location: cfg.Location,
			}),
			tools.BroadcastTool(tools.BroadcastOptions{
				Groups: store,
				Drafts: store,
				Sender: groupSender{client: bot.Client},
			}),
			tools.RulesTool(tools.RulesOptions{
				Store:    store,
				Admins:   groupMembers{client: bot.Client, botID: bot.User.ID},
				BotID:    bot.User.ID,
				Location: cfg.Location,
			}),
		)
	}
	return builtin
}

// imageTool draws with Gemini and counts pictures in the database.
func imageTool(cfg config.Config, store *storage.Store) tools.Tool {
	return tools.ImageTool(tools.ImageOptions{
		Generator: ai.NewImageClient(ai.ImageOptions{
			BaseURL: cfg.ImageBaseURL,
			APIKey:  cfg.AIAPIKey,
			Model:   cfg.ImageModel,
		}),
		Quota:      store,
		DailyLimit: cfg.DailyImageLimit,
		Location:   cfg.Location,
	})
}

// withBackup puts the backup provider behind the main one when
// BACKUP_API_KEY is set. Voice messages are still transcribed by the main
// one only.
func withBackup(cfg config.Config, model *ai.Client, logger *slog.Logger) agent.Model {
	if cfg.BackupAPIKey == "" {
		return model
	}
	backup := ai.NewClient(ai.Options{
		BaseURL:        cfg.BackupBaseURL,
		APIKey:         cfg.BackupAPIKey,
		Model:          cfg.BackupModel,
		MaxTokens:      cfg.MaxOutputTokens,
		MaxAttempts:    2,
		HTTPClient:     &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout: attemptTimeout,
		Logger:         logger,
	})
	return ai.NewChain(model, backup, logger)
}

// transcriber is the model client when the provider can listen to audio,
// and nil otherwise, which tells the service it cannot hear voice messages.
func transcriber(cfg config.Config, model *ai.Client) service.Transcriber {
	if !cfg.AudioInput {
		return nil
	}
	return model
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

// Retention is how long chat history is kept, from RETENTION_DAYS. Zero
// keeps it forever.
func Retention(cfg config.Config) time.Duration {
	return time.Duration(cfg.RetentionDays) * 24 * time.Hour
}

// Cleanup is the endpoint Vercel Cron calls to delete old history.
type Cleanup struct {
	Handler http.Handler
	// Close releases the database pool.
	Close func()
}

// NewCleanup builds the scheduled-cleanup endpoint. Each authorised call
// deletes messages older than RETENTION_DAYS and reports what it removed.
func NewCleanup(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Cleanup, error) {
	if cfg.CronSecret == "" {
		return nil, errors.New("CRON_SECRET is required for the cleanup endpoint")
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	handler, err := cron.NewHandler(cron.Options{
		Secret: cfg.CronSecret,
		Logger: logger,
		Run: func(ctx context.Context) (any, error) {
			return expire(ctx, cfg, store, logger)
		},
	})
	if err != nil {
		store.Close()
		return nil, err
	}
	return &Cleanup{Handler: handler, Close: store.Close}, nil
}

// expire applies the retention policy once.
func expire(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) (storage.Expired, error) {
	if cfg.RetentionDays == 0 {
		return storage.Expired{}, nil
	}
	expired, err := store.Expire(ctx, Retention(cfg))
	if err != nil {
		return storage.Expired{}, err
	}
	logger.Info("old history deleted", "messages", expired.Messages, "conversations", expired.Conversations,
		"older_than_days", cfg.RetentionDays)
	return expired, nil
}

// Expire applies the retention policy once; the long-polling command calls
// it on a timer.
func Expire(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) error {
	_, err := expire(ctx, cfg, store, logger)
	return err
}
