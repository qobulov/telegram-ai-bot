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
	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/cron"
	"telegram-ai-bot/internal/game"
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

// attemptTimeout bounds one request to the model provider. At 20 seconds two
// busy models used 40 of a turn's 55 before the fallback that answered was
// asked, and the member got "Javob berish juda cho'zilib ketdi" (2026-11-20);
// a model that answers at all does so well within 12.
const attemptTimeout = 12 * time.Second

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

// AdminRights says whether a member administers a group and the admin
// rights they hold there, for /rights.
func (m groupMembers) AdminRights(ctx context.Context, chatID, userID int64) (bool, []string, error) {
	member, err := m.client.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, nil, err
	}
	return member.IsAdmin(), member.Rights(), nil
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

// GroupCreator is the user ID of whoever created a group, 0 when the
// admins Telegram lists show none.
func (m groupMembers) GroupCreator(ctx context.Context, chatID int64) (int64, error) {
	admins, err := m.client.GetChatAdministrators(ctx, chatID)
	if err != nil {
		return 0, err
	}
	for _, admin := range admins {
		if admin.Status == "creator" && admin.User != nil {
			return admin.User.ID, nil
		}
	}
	return 0, nil
}

// HoldsRight reports whether a member holds an admin right in a group,
// for the join check its admins who may invite users may turn on and off.
func (m groupMembers) HoldsRight(ctx context.Context, chatID, userID int64, right string) (bool, error) {
	member, err := m.client.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	return member.Has(right), nil
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

// botAccount is the bot's own Telegram profile, its photo found by its ID.
type botAccount struct {
	*telegram.Client
	id int64
}

func (b botAccount) Photo(ctx context.Context) ([]byte, string, bool, error) {
	return b.ProfilePhoto(ctx, b.id, maxAvatarBytes)
}

// memberPhoto downloads a member's profile photo, nil without a client.
func memberPhoto(bot Bot) func(ctx context.Context, userID int64) ([]byte, string, bool, error) {
	if bot.Client == nil {
		return nil
	}
	return func(ctx context.Context, userID int64) ([]byte, string, bool, error) {
		return bot.Client.ProfilePhoto(ctx, userID, maxAvatarBytes)
	}
}

// maxAvatarBytes bounds the profile photo downloaded to be described.
const maxAvatarBytes = 2 << 20

// groupSender says goodbye in a group or a channel and leaves it, for the
// leave_chat tool.
type groupSender struct {
	client *telegram.Client
}

func (g groupSender) SendToGroup(ctx context.Context, chatID int64, text string) error {
	_, err := g.client.SendMessage(ctx, telegram.Outgoing{ChatID: chatID, Text: text})
	return err
}

func (g groupSender) LeaveChat(ctx context.Context, chatID int64) error {
	return g.client.LeaveChat(ctx, chatID)
}

// pinSender sends the send_location tool's pins as venues: a pin on the
// map with the place's name over it.
type pinSender struct {
	client *telegram.Client
}

func (p pinSender) SendPin(ctx context.Context, pin webapi.Pin) error {
	_, err := p.client.SendVenue(ctx, telegram.Venue{
		ChatID: pin.ChatID, ReplyToMessageID: pin.ReplyToMessageID,
		Latitude: pin.Lat, Longitude: pin.Lon, Title: pin.Title, Address: pin.Address,
	})
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
	record := usageRecorder(store, logger)
	moderation := tools.NewModeration(tools.ModerationOptions{
		BotID:    bot.User.ID,
		Members:  bot.Client,
		Telegram: bot.Client,
		Store:    store,
		Rights:   store,
		Warns:    store,
		Ranks:    store,
		Bans:     store,
		Location: cfg.Location,
		// A username can change hands; only an ID makes an owner a moderator.
		OwnersByID: len(cfg.OwnerIDs) > 0,
		Owners:     cfg.OwnerIDs,
		Usernames:  store,
		Roster:     store,
		Targets:    store,
		Links:      store,
		Messages:   store,
	})
	if len(cfg.OwnerUsernames) > 0 && len(cfg.OwnerIDs) == 0 {
		logger.Warn("BOT_OWNER_IDS is not set: owners are known only by username, which can change hands, so they moderate only with their own admin rights")
	}
	eyes := stickerLabeller(cfg, logger, record)
	available := append(builtinTools(cfg, store, bot, moderation.Tool(), eyes), liveDataTools(cfg, store, bot)...)
	available = append(available, moderation.TopicsTool(bot.Client), tools.BotProfileTool(tools.BotProfileOptions{
		Account: botAccount{Client: bot.Client, id: bot.User.ID},
		Eyes:    eyes,
	}))
	// Gifts spend the bot's Stars, so only owners known by ID may send them:
	// a username can change hands.
	if len(cfg.OwnerIDs) > 0 {
		available = append(available, tools.GiftTool(tools.GiftOptions{Shop: bot.Client, Members: moderation, BotID: bot.User.ID}))
	}
	if len(cfg.OwnerUsernames) > 0 {
		available = append(available, stickerSetTool(cfg, store, bot, logger))
	}
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
		AttemptTimeout:   attemptTimeout,
		FallbackModels:   cfg.FallbackModels,
		ReasoningEffort:  cfg.ReasoningEffort,
		TranscribeEffort: transcribeReasoning(cfg),
		Logger:           logger,
		// The persona and the tools, the same each turn, are cached on
		// Gemini and paid at a tenth.
		PromptCache: true,
		Purpose:     "chat",
		Record:      record,
	})
	router, chat := chatRouting(cfg, logger, record)
	assistant := agent.New(agent.Options{
		Store:        store,
		Model:        model,
		Tools:        registry,
		Router:       router,
		ChatModel:    chat,
		Name:         cfg.BotDisplayName,
		Persona:      withNewHeroes(cfg, bot),
		GroupNote:    cfg.GroupNote,
		OwnerNote:    cfg.OwnerNote,
		MaxHistory:   cfg.MaxHistoryMessages,
		MaxToolCalls: cfg.MaxToolCallsPerTurn,
	})
	opts := service.Options{
		Sender:          bot.Client,
		Replier:         assistant,
		Tools:           registry,
		Recorder:        store,
		Members:         groupMembers{client: bot.Client, botID: bot.User.ID},
		Groups:          store,
		Roster:          store,
		Invites:         store,
		Words:           store,
		Teams:           store,
		Ranks:           store,
		BanList:         store,
		BanVotes:        store,
		MemberLinks:     store,
		Reports:         store,
		Extras:          store,
		WarnList:        store,
		LinkMaker:       bot.Client,
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
		FootballEnabled:    cfg.FootballDataToken != "",

		GroupReplies:          store,
		GroupRatePerMinute:    cfg.GroupRateLimitPerMinute,
		MemberRepliesPer10Min: cfg.MemberRepliesPer10Min,
		Skips:                 store,
		AILimits:              store,
		Failures:              store,
		PrivateQuota:          store,
		PrivateDailyLimit:     cfg.PrivateDailyLimit,
		Payments:              bot.Client,
		Crystals:              store,
		Mail:                  store,
		Premium:               store,
		Cards:                 store,
		PremiumPrice:          cfg.PremiumPrice,
		PremiumDays:           cfg.PremiumDays,
		PremiumDailyLimit:     cfg.PremiumDailyLimit,
		PrivateChats:          cfg.PrivateChats,
		Reactor:               bot.Client,
		OwnerMenu:             ownerGroupMenu,
		MenuSetter:            bot.Client,
		RankMenu:              RankMenu,
		AdminMenu:             GroupAdminMenu,
		AdminMenuSetter:       bot.Client,
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
		PictureJudge:     pictureJudge(cfg, logger, record),
		PictureVerdicts:  store,
		VoiceTranscripts: store,
		Judge:            swearJudge(cfg, logger, record),
		Sworn:            store,
		Joins:            store,
		JoinGate:         bot.Client,
		Boosts:           store,
		Stickers:         store,
		ClipSender:       bot.Client,
		AudioSender:      bot.Client,
		Files:            bot.Client,
		StickerSender:    bot.Client,
		StickerImporter:  &stickers.Importer{Telegram: bot.Client, Store: store, Model: stickerLabeller(cfg, logger, record), Workers: stickerWorkers, Logger: logger},
		Answers:          store,
		Inline:           bot.Client,

		Business:           store,
		BusinessLookup:     bot.Client,
		BusinessDailyLimit: cfg.BusinessDailyLimit,
	}
	if cfg.GiphyAPIKey != "" {
		opts.GIFs = &gifs.Client{APIKey: cfg.GiphyAPIKey, HTTPClient: &http.Client{Timeout: giphyTimeout}}
		opts.AnimationSender, opts.MediaSlots = bot.Client, store
		opts.PraiseGIFs = service.PraiseGIFLinks(cfg.WebAppURL)
	}
	if g := newGame(cfg); g != nil {
		opts.StatsURL = statsPage(cfg)
		opts.CardsURL = cardsPage(cfg)
		opts.KassaURL = kassaPage(cfg)
		opts.Game, opts.GameTable = g, bot.Client
	}
	if mode.Serverless {
		opts.Workers, opts.QueueSize = 0, 0
		opts.MutedChats = nil
		opts.MuteStore = store
	}
	return service.New(opts), nil
}

// builtinTools are the tools the model may call in this deployment: the help
// follows what is enabled, and the time is the groups'.
// moderate is made by the caller, which also hands its confirmations to the
// service.
// eyes look at members' profile photos; nil leaves them unseen.
func builtinTools(cfg config.Config, store *storage.Store, bot Bot, moderate tools.Tool, eyes tools.Eyes) []tools.Tool {
	members := tools.MemberInfoOptions{Store: store, Telegram: bot.Client, Tokens: cards.Tokens, NextToken: func(now time.Time) time.Time { return cards.Today(now).Next() }, Location: cfg.Location,
		Photo: memberPhoto(bot), Eyes: eyes}
	help := service.HelpText(service.Abilities{Listen: cfg.AudioInput, Football: cfg.FootballDataToken != "", Pages: cfg.WebAppURL != ""})
	builtin := tools.Builtin(help, cfg.Location)
	if cfg.WebAppURL != "" {
		builtin = append(builtin, tools.GameTool(tools.GameOptions{Sender: bot.Client, Store: store, Games: []string{game.OtinchiShortName, game.BossShortName, game.MafiaShortName}}))
	}
	// Any group's admins, and any member for a poll or the admin list, may
	// use these, so they are there with or without owners.
	builtin = append(builtin, moderate,
		tools.PollTool(tools.PollOptions{Sender: bot.Client, Store: store, Members: bot.Client, BotID: bot.User.ID, OwnersByID: len(cfg.OwnerIDs) > 0}),
		tools.DiceTool(tools.DiceOptions{Sender: bot.Client, Store: store}),
		tools.AdminsTool(tools.AdminsOptions{Telegram: bot.Client, BotID: bot.User.ID, Owners: cfg.OwnerIDs}),
		tools.MembersTool(tools.MembersOptions{Store: store, Telegram: bot.Client, Owners: cfg.OwnerIDs, Location: cfg.Location}),
		tools.ProfileTool(tools.ProfileOptions{Store: store}),
		tools.CatchUpTool(tools.CatchUpOptions{Store: store, Location: cfg.Location}),
		tools.GroupStatsTool(tools.GroupStatsOptions{Store: store, BotID: bot.User.ID, Location: cfg.Location}),
		tools.InviteStatsTool(tools.InviteStatsOptions{Store: store, Location: cfg.Location}),
		tools.GroupInfoTool(bot.Client, bot.User.ID, store),
		tools.MemberInfoTool(members),
		tools.ShipTool(members),
		tools.PunishmentsTool(tools.PunishmentsOptions{Store: store, Creators: groupMembers{client: bot.Client, botID: bot.User.ID}, BotID: bot.User.ID, Location: cfg.Location}),
		tools.JoinCaptchaTool(tools.JoinCaptchaOptions{Store: store, Rights: groupMembers{client: bot.Client, botID: bot.User.ID}}),
	)
	// Only owners known by ID can be written to, and only they are. Only
	// their business accounts are used, too: the account speaks for whoever
	// holds it.
	if len(cfg.OwnerIDs) > 0 {
		builtin = append(builtin, tools.NotifyOwnerTool(tools.NotifyOptions{
			Quota:    store,
			Telegram: bot.Client,
			Owners:   slices.Sorted(maps.Keys(cfg.OwnerIDs)),
			Location: cfg.Location,
		}),
			tools.StoryTool(tools.StoryOptions{Accounts: store, Poster: bot.Client}),
			tools.ChecklistTool(tools.ChecklistOptions{Accounts: store, Sender: bot.Client, Location: cfg.Location}),
		)
	}
	if len(cfg.OwnerUsernames) > 0 {
		builtin = append(builtin,
			tools.DatabaseTool(tools.DatabaseOptions{
				Store:    store,
				Members:  groupMembers{client: bot.Client, botID: bot.User.ID},
				Location: cfg.Location,
			}),
			tools.LeaveTool(tools.LeaveOptions{
				Groups:   store,
				Channels: store,
				Telegram: groupSender{client: bot.Client},
			}),
			tools.RulesTool(tools.RulesOptions{
				Store:    store,
				Admins:   groupMembers{client: bot.Client, botID: bot.User.ID},
				BotID:    bot.User.ID,
				Location: cfg.Location,
				Groups:   store,
				Muter:    store,

				CanCheckLinks: checker(cfg).CanCheckLinks(),
				// The bot's own model judges what Sightengine cannot, or
				// every picture without its keys.
				CanCheckImages: true,
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
		CASKey:            cfg.CASKey,
	})
}

// usageRecord saves what a model request cost.
type usageRecord = func(context.Context, ai.Usage)

// usageWriteTimeout bounds saving a request's usage, which runs before the
// answer goes out: on serverless hosting nothing may be left running.
const usageWriteTimeout = 2 * time.Second

// usageRecorder saves every model request's tokens in model_usage. A save
// that fails is logged and the answer goes on.
func usageRecorder(store *storage.Store, logger *slog.Logger) usageRecord {
	return func(ctx context.Context, u ai.Usage) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), usageWriteTimeout)
		defer cancel()
		if err := store.RecordModelUsage(ctx, u); err != nil {
			logger.Warn("could not save model usage", "purpose", u.Purpose, "model", u.Model, "error", err)
		}
	}
}

// pictureJudgeTokens is room for the picture judge's one word and its
// thinking: Gemini thinks about a picture even at low effort.
const pictureJudgeTokens = 1024

// pictureJudge is the model asked whether a picture is nude when
// Sightengine cannot say: ROUTER_MODEL, a light and cheap one, when it is
// set, with the main model and its fallbacks behind it when it is busy;
// the main model otherwise. A one-word answer, so it thinks little.
// Gemini's light models see pictures as the main ones do.
func pictureJudge(cfg config.Config, logger *slog.Logger, record usageRecord) *ai.Client {
	model, fallbacks := cfg.Model, cfg.FallbackModels
	if cfg.RouterModel != "" {
		model, fallbacks = cfg.RouterModel, append([]string{cfg.Model}, cfg.FallbackModels...)
	}
	return ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           model,
		FallbackModels:  fallbacks,
		ReasoningEffort: judgeReasoning(cfg.ReasoningEffort),
		MaxTokens:       pictureJudgeTokens,
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
		Purpose:         "picture_judge",
		Record:          record,
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
// times, news, Wikipedia, crypto, and football when a token is set, send
// places as pins on the map, and recognise songs, counted in the database,
// when a RapidAPI key is set. The user agent gives the bot's t.me link as
// the contact some APIs ask for.
func liveDataTools(cfg config.Config, store *storage.Store, bot Bot) []tools.Tool {
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
		Pins:            pinSender{client: bot.Client},
		ShazamKey:       cfg.RapidAPIKey,
		Files:           bot.Client,
		SongQuota:       store,
	}
	return webapi.Tools(opts)
}

// stickerSetTool lets owners fill the bot's own sticker set from photos;
// the stickers it adds are looked at as an imported pack's are, so that she
// sends them too.
func stickerSetTool(cfg config.Config, store *storage.Store, bot Bot, logger *slog.Logger) tools.Tool {
	opts := tools.StickerSetOptions{
		Telegram:    bot.Client,
		Store:       store,
		Learner:     &stickers.Importer{Telegram: bot.Client, Store: store, Model: stickerLabeller(cfg, logger, usageRecorder(store, logger)), Workers: 1, Logger: logger},
		BotUsername: bot.User.Username,
		Title:       cfg.BotDisplayName,
		Location:    cfg.Location,
	}
	return tools.StickerSetTool(opts)
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
func swearJudge(cfg config.Config, logger *slog.Logger, record usageRecord) *ai.Client {
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
		Purpose:         "swear_judge",
		Record:          record,
	})
}

// chatRouting is the router of ROUTER_MODEL and the chat model of
// CHAT_MODEL, both nil without a router. The router gives one word, as the
// swear judge does; a chat model that fails hands the turn to the main
// model and its fallbacks.
func chatRouting(cfg config.Config, logger *slog.Logger, record usageRecord) (router, chat agent.Model) {
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
		Purpose:         "router",
		Record:          record,
	})
	chat = ai.NewClient(ai.Options{
		BaseURL:         cfg.AIBaseURL,
		APIKey:          cfg.AIAPIKey,
		Model:           cfg.ChatModel,
		FallbackModels:  cfg.FallbackModels,
		ReasoningEffort: cfg.ChatReasoningEffort,
		MaxTokens:       cfg.MaxOutputTokens,
		MaxAttempts:     2,
		HTTPClient:      &http.Client{Timeout: cfg.RequestTimeout},
		AttemptTimeout:  attemptTimeout,
		Logger:          logger,
		Purpose:         "chat_light",
		Record:          record,
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

// transcribeReasoning is how hard the model thinks before it writes a
// voice message down: minimal, unless thinking is off. At low it thought
// 150 to 1,700 tokens over transcripts of 6 to 80 (logs of 2026-11-19),
// nearly all that a voice message cost, for words that are heard, not
// worked out. Only Gemini hears voice messages; other providers keep
// ReasoningEffort.
func transcribeReasoning(cfg config.Config) string {
	if !cfg.AudioInput || cfg.ReasoningEffort == "none" {
		return cfg.ReasoningEffort
	}
	return "minimal"
}

// stickerLabeller is the model that looks at stickers on import. It gets
// more room than an answer: Gemini spends output tokens thinking, and with
// a small limit a fifth of its labels came back cut off.
func stickerLabeller(cfg config.Config, logger *slog.Logger, record usageRecord) *ai.Client {
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
		Purpose:         "sticker_labeller",
		Record:          record,
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
	SetOwnerMenus(ctx, bot.Client, cfg.OwnerIDs, logger)
	SetMenuApp(ctx, bot.Client, cfg, logger)
	SetAdminRights(ctx, bot.Client, logger)
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
// RETENTION_DAYS, pays the arena's last week if it is not paid yet, and
// reports what it removed.
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
			awards := service.InviteAwardOptions{Book: store, Telegram: client, Location: cfg.Location, Now: time.Now(), Logger: logger}
			if _, err := service.PostInviteAwards(ctx, awards); err != nil {
				logger.Warn("could not post the invite awards", "error", err)
			}
			week := service.WeekAwardOptions{Book: store, Telegram: client, Location: cfg.Location, Now: time.Now(), NotLucky: cfg.OwnerIDs, Logger: logger}
			if posted, err := service.PostWeekAwards(ctx, week); err != nil {
				logger.Warn("could not post the week's awards", "error", err)
			} else if posted > 0 {
				logger.Info("posted the week's awards", "groups", posted)
			}
			if paid, err := cards.PayArenaWeek(ctx, store, time.Now()); err != nil {
				logger.Warn("could not pay the arena week", "error", err)
			} else if paid > 0 {
				logger.Info("paid the arena week", "players", paid)
			}
			if paid, err := cards.PayBossWeek(ctx, store, time.Now()); err != nil {
				logger.Warn("could not pay the boss week", "error", err)
			} else if paid > 0 {
				logger.Info("paid the boss week", "players", paid)
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

// quietGroupKeep is how long a group's messages are kept where the bot has
// not answered for quietGroupDays, whatever RETENTION_DAYS says: the week's
// statistics, and a day more.
const (
	quietGroupKeep = 8 * 24 * time.Hour
	quietGroupDays = 7 * 24 * time.Hour
	// noAIGroupKeep is how long a group without AI keeps its lines, which
	// have no words and serve the flood guard's minute alone.
	noAIGroupKeep = 24 * time.Hour
)

// expire applies the retention policy once.
func expire(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) (storage.Expired, error) {
	if deleted, err := store.ExpireQuietGroups(ctx, time.Now().Add(-quietGroupKeep), time.Now().Add(-quietGroupDays)); err != nil {
		logger.Warn("could not trim the quiet groups' logs", "error", err)
	} else if deleted > 0 {
		logger.Info("quiet groups' old messages deleted", "messages", deleted)
	}
	if deleted, err := store.ExpireGroupsWithoutAI(ctx, time.Now().Add(-noAIGroupKeep)); err != nil {
		logger.Warn("could not trim the logs of groups without AI", "error", err)
	} else if deleted > 0 {
		logger.Info("old messages of groups without AI deleted", "messages", deleted)
	}
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
