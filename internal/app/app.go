// Package app assembles the bot from its parts. Both ways of running it use
// this: the long-polling command (cmd/bot) and the webhook function (api).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/cron"
	"telegram-ai-bot/internal/gifs"
	"telegram-ai-bot/internal/guards"
	"telegram-ai-bot/internal/service"
	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
	"telegram-ai-bot/internal/webapi"
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

// IsGroupCreator reports whether a member created a group, for the
// punishments a group's creator may set.
func (m groupMembers) IsGroupCreator(ctx context.Context, chatID, userID int64) (bool, error) {
	member, err := m.client.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	return member.Status == "creator", nil
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
// botAccount is the bot's own Telegram profile, its photo found by its ID.
type botAccount struct {
	*telegram.Client
	id int64
}

func (b botAccount) Photo(ctx context.Context) ([]byte, string, bool, error) {
	return b.ProfilePhoto(ctx, b.id, maxAvatarBytes)
}

// maxAvatarBytes bounds the profile photo downloaded to be described.
const maxAvatarBytes = 2 << 20

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
	moderation := tools.NewModeration(tools.ModerationOptions{
		BotID:    bot.User.ID,
		Members:  bot.Client,
		Telegram: bot.Client,
		Store:    store,
		Rights:   store,
		Location: cfg.Location,
		// A username can change hands; only an ID makes an owner a moderator.
		OwnersByID: len(cfg.OwnerIDs) > 0,
		Owners:     cfg.OwnerIDs,
		Usernames:  store,
		Roster:     store,
		Messages:   store,
	})
	if len(cfg.OwnerUsernames) > 0 && len(cfg.OwnerIDs) == 0 {
		logger.Warn("BOT_OWNER_IDS is not set: owners are known only by username, which can change hands, so they moderate only with their own admin rights")
	}
	available := append(builtinTools(cfg, store, bot, moderation.Tool()), liveDataTools(cfg, bot)...)
	available = append(available, tools.BotProfileTool(tools.BotProfileOptions{
		Account: botAccount{Client: bot.Client, id: bot.User.ID},
		Eyes:    stickerLabeller(cfg, logger),
	}))
	registry, err := tools.NewRegistry(cfg.IsAdmin, logger, available...)
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
		AttemptTimeout:  attemptTimeout,
		FallbackModels:  cfg.FallbackModels,
		ReasoningEffort: cfg.ReasoningEffort,
		Logger:          logger,
	})
	router, chat := chatRouting(cfg, logger)
	assistant := agent.New(agent.Options{
		Store:        store,
		Model:        model,
		Tools:        registry,
		Router:       router,
		ChatModel:    chat,
		Name:         cfg.BotDisplayName,
		Persona:      withNewHeroes(cfg, bot),
		GroupNote:    cfg.GroupNote,
		MaxHistory:   cfg.MaxHistoryMessages,
		MaxToolCalls: cfg.MaxToolCallsPerTurn,
	})
	opts := service.Options{
		Sender:          bot.Client,
		Replier:         assistant,
		Recorder:        store,
		Members:         groupMembers{client: bot.Client, botID: bot.User.ID},
		Groups:          store,
		Roster:          store,
		Profiles:        store,
		Exchanges:       store,
		MemberCount:     bot.Client,
		Bots:            store,
		MinGroupMembers: cfg.MinGroupMembers,
		Muter:           store,
		Logger:          logger,
		Model:           cfg.Model,
		Workers:         cfg.MaxConcurrentRequests,
		QueueSize:       queuePerWorker,
		RatePerMinute:   cfg.UserRateLimitPerMinute,
		MaxInputChars:   cfg.MaxInputChars,
		TurnTimeout:     cfg.RequestTimeout,
		IsAdmin:         cfg.IsAdmin,
		AdminCommands:   cfg.AdminCommands,

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
		OwnerPresence:      store,
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
		FootballEnabled:    cfg.FootballDataToken != "",

		GroupReplies:          store,
		GroupRatePerMinute:    cfg.GroupRateLimitPerMinute,
		MemberRepliesPer10Min: cfg.MemberRepliesPer10Min,
		Skips:                 store,
		PrivateQuota:          store,
		PrivateDailyLimit:     cfg.PrivateDailyLimit,
		PrivateChats:          cfg.PrivateChats,
		Reactor:               bot.Client,
		OwnerMenu:             ownerMenu,
		MenuSetter:            bot.Client,
		Settings:              store,
		CheckSetting: func(setting config.Setting, value string) error {
			return config.CheckSetting(os.Getenv, setting, value)
		},
		Getenv:           os.Getenv,
		ChatActor:        bot.Client,
		BotRights:        botRights{store: store, members: bot.Client, botID: bot.User.ID, logger: logger},
		Rules:            store,
		Strikes:          store,
		AdminList:        bot.Client,
		RequiredChannels: cfg.RequiredChannels,
		QuickAnswers:     true,
		MinActiveMembers: cfg.MinActiveMembers,
		ActiveWindow:     time.Duration(cfg.ActiveDays) * 24 * time.Hour,
		ActiveCounter:    store,
		Channels:         bot.Client,
		Buttons:          bot.Client,
		Restrictor:       bot.Client,
		Guards:           checker(cfg),
		GroupModerator:   bot.Client,
		Judge:            swearJudge(cfg, logger),
		Sworn:            store,
		Joins:            store,
		JoinGate:         bot.Client,
		Stickers:         store,
		ClipSender:       bot.Client,
		AudioSender:      bot.Client,
		Files:            bot.Client,
		StickerSender:    bot.Client,
		StickerImporter:  &stickers.Importer{Telegram: bot.Client, Store: store, Model: stickerLabeller(cfg, logger), Workers: stickerWorkers, Logger: logger},
		Answers:          store,
	}
	if cfg.GiphyAPIKey != "" {
		opts.GIFs = &gifs.Client{APIKey: cfg.GiphyAPIKey, HTTPClient: &http.Client{Timeout: giphyTimeout}}
		opts.AnimationSender, opts.MediaSlots = bot.Client, store
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
// moderate is made by the caller, which also hands its confirmations to the
// service.
func builtinTools(cfg config.Config, store *storage.Store, bot Bot, moderate tools.Tool) []tools.Tool {
	help := service.HelpText(service.Abilities{Draw: cfg.ImagesEnabled, Listen: cfg.AudioInput, Football: cfg.FootballDataToken != ""})
	builtin := tools.Builtin(help, cfg.Location)
	if cfg.ImagesEnabled {
		builtin = append(builtin, imageTool(cfg, store))
	}
	// Any group's admins, and any member for a poll or the admin list, may
	// use these, so they are there with or without owners.
	builtin = append(builtin, moderate,
		tools.PollTool(tools.PollOptions{Sender: bot.Client, Store: store}),
		tools.AdminsTool(tools.AdminsOptions{Telegram: bot.Client, BotID: bot.User.ID, Owners: cfg.OwnerIDs}),
		tools.MembersTool(tools.MembersOptions{Store: store, Telegram: bot.Client, Owners: cfg.OwnerIDs, Location: cfg.Location}),
		tools.ProfileTool(tools.ProfileOptions{Store: store}),
		tools.CatchUpTool(tools.CatchUpOptions{Store: store, Location: cfg.Location}),
		tools.GroupStatsTool(tools.GroupStatsOptions{Store: store, BotID: bot.User.ID, Location: cfg.Location}),
		tools.PunishmentsTool(tools.PunishmentsOptions{Store: store, Creators: groupMembers{client: bot.Client, botID: bot.User.ID}, BotID: bot.User.ID, Location: cfg.Location}),
	)
	// Only owners known by ID can be written to, and only they are.
	if len(cfg.OwnerIDs) > 0 {
		builtin = append(builtin, tools.NotifyOwnerTool(tools.NotifyOptions{
			Quota:    store,
			Telegram: bot.Client,
			Owners:   slices.Sorted(maps.Keys(cfg.OwnerIDs)),
			Location: cfg.Location,
		}))
	}
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
				Groups:   store,
				Muter:    store,

				CanCheckLinks:  checker(cfg).CanCheckLinks(),
				CanCheckImages: checker(cfg).CanCheckImages(),
			}),
		)
	}
	return builtin
}

// checker asks the spam lists, Google Safe Browsing, URLhaus and
// Sightengine for the guards.
func checker(cfg config.Config) *guards.Checker {
	return guards.New(guards.Options{
		URLhausKey:        cfg.URLhausAuthKey,
		SafeBrowsingKey:   cfg.SafeBrowsingKey,
		SightengineUser:   cfg.SightengineUser,
		SightengineSecret: cfg.SightengineSecret,
	})
}

// newHeroesWait bounds asking MLBBDex for the newest heroes at start.
const newHeroesWait = 3 * time.Second

// withNewHeroes is the persona with the Mobile Legends heroes released
// lately, which the model does not know: it denied Marcel and left him out
// of comparisons. Without an answer the persona goes as it is.
func withNewHeroes(cfg config.Config, bot Bot) string {
	if !cfg.NewHeroes {
		return cfg.PersonaPrompt
	}
	ctx, cancel := context.WithTimeout(context.Background(), newHeroesWait)
	defer cancel()
	note := webapi.NewHeroesNote(ctx, webapi.Options{UserAgent: "MariaBot/1.0 (+https://t.me/" + bot.User.Username + ")"}, time.Now())
	if note == "" {
		return cfg.PersonaPrompt
	}
	return cfg.PersonaPrompt + "\n\n" + note
}

// liveDataTools look things up on public web APIs: rates, weather, prayer
// times, news, Wikipedia, crypto, and football when a token is set. The
// user agent gives the bot's t.me link as the contact some APIs ask for.
func liveDataTools(cfg config.Config, bot Bot) []tools.Tool {
	opts := webapi.Options{
		UserAgent:       "MariaBot/1.0 (+https://t.me/" + bot.User.Username + ")",
		Location:        cfg.Location,
		FootballToken:   cfg.FootballDataToken,
		CoinGeckoKey:    cfg.CoinGeckoDemoKey,
		TavilyKey:       cfg.TavilyAPIKey,
		TMDBToken:       cfg.TMDBToken,
		YouTubeKey:      cfg.YouTubeAPIKey,
		CalendarificKey: cfg.CalendarificAPIKey,
		ImgflipKey:      cfg.ImgflipAPIKey,
	}
	return webapi.Tools(opts)
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

// giphyTimeout bounds a GIF search: a GIF that is late is not worth the wait.
const giphyTimeout = 5 * time.Second

// stickerWorkers is how many stickers the model looks at at once on import:
// enough to label a pack of a hundred within one serverless request or two.
const stickerWorkers = 10

// judgeTokens is room for the judge's one word and a little thinking.
const judgeTokens = 512

// judgeEffort is how hard the judge thinks, whatever AI_REASONING_EFFORT
// asks of answers: at medium, its thinking used up judgeTokens and the
// judge's word was cut off.
const judgeEffort = "low"

// swearJudge asks whom swearing is aimed at: a one-word answer to a short
// prompt, so it gets few tokens and thinks little, on the main model and
// its fallbacks, which judge better than a lighter one.
func swearJudge(cfg config.Config, logger *slog.Logger) *ai.Client {
	return ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           cfg.Model,
		FallbackModels:  cfg.FallbackModels,
		ReasoningEffort: judgeReasoning(cfg.ReasoningEffort),
		MaxTokens:       judgeTokens,
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
	})
}

// chatRouting is the router of ROUTER_MODEL and the chat model of
// CHAT_MODEL, both nil without a router. The router gives one word, as the
// swear judge does; a chat model that fails hands the turn to the main
// model and its fallbacks.
func chatRouting(cfg config.Config, logger *slog.Logger) (router, chat agent.Model) {
	if cfg.RouterModel == "" {
		return nil, nil
	}
	router = ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           cfg.RouterModel,
		ReasoningEffort: judgeReasoning(cfg.ReasoningEffort),
		MaxTokens:       judgeTokens,
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
	})
	chat = ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           cfg.ChatModel,
		FallbackModels:  cfg.FallbackModels,
		ReasoningEffort: cfg.ReasoningEffort,
		MaxTokens:       cfg.MaxOutputTokens,
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
	})
	return router, chat
}

// judgeReasoning is judgeEffort, unless thinking is off or left to the
// model's default: the judge never thinks more than answers do.
func judgeReasoning(effort string) string {
	switch effort {
	case "", "none", "minimal", "low":
		return effort
	}
	return judgeEffort
}

// stickerLabeller is the model that looks at stickers on import. It gets
// more room than an answer: Gemini spends output tokens thinking, and with
// a small limit a fifth of its labels came back cut off.
func stickerLabeller(cfg config.Config, logger *slog.Logger) *ai.Client {
	return ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           cfg.Model,
		ReasoningEffort: cfg.ReasoningEffort,
		MaxTokens:       max(cfg.MaxOutputTokens, 2048),
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
	})
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
	SetCommandMenus(ctx, bot.Client, logger)
	SetProfileTexts(ctx, bot.Client, logger)
	if changed, err := bot.Client.EnsureAllowedUpdates(ctx, cfg.WebhookSecret); err != nil {
		logger.Warn("could not check the webhook's update kinds", "error", err)
	} else if changed {
		logger.Info("webhook set again to receive every update kind the bot handles")
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
// declines the join requests whose button was not pressed in time, which
// the webhook's updates did not notice, deletes messages older than
// RETENTION_DAYS, and reports what it removed.
func NewCleanup(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Cleanup, error) {
	if cfg.CronSecret == "" {
		return nil, errors.New("CRON_SECRET is required for the cleanup endpoint")
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	handler, err := cron.NewHandler(cron.Options{
		Secret: cfg.CronSecret,
		Logger: logger,
		Run: func(ctx context.Context) (any, error) {
			if _, err := service.DeclineLateJoins(ctx, store, client, store, time.Now(), logger); err != nil {
				logger.Warn("could not decline the late join requests", "error", err)
			}
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
