// Package config loads and validates the bot's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	groupContextShared     = "shared"
	telegramMessageLength  = 4096
	maxAmbientMessages     = 500
	maxWebhookSecretLength = 256
	minCronSecretLength    = 16
)

// removedVariables maps settings that no longer exist to their replacement,
// so that an old configuration fails loudly instead of being half ignored.
var removedVariables = map[string]string{
	"SYSTEM_PROMPT":               "PERSONA_PROMPT",
	"GROUP_SYSTEM_PROMPT":         "PERSONA_PROMPT",
	"ENABLE_MENTION_TRIGGER":      "ENABLE_USERNAME_MENTION",
	"ENABLE_DISPLAY_NAME_TRIGGER": "ENABLE_WAKE_WORD_TRIGGER",
}

// Config holds every runtime setting. It is read once at startup and never mutated.
type Config struct {
	TelegramToken string
	// WebhookSecret is needed only when updates arrive by webhook.
	WebhookSecret string
	// CronSecret is what Vercel Cron sends to authenticate scheduled calls.
	CronSecret     string
	TelegramAPIURL string
	// The model provider. Gemini and xAI are both reached through the same
	// OpenAI-compatible chat completions API; only these differ.
	AIProvider string
	// ReasoningEffort is how hard Gemini thinks before it answers: none,
	// minimal, low, medium or high, from AI_REASONING_EFFORT; "" leaves it
	// to the model. Thinking is paid as output, so it defaults to low.
	ReasoningEffort string
	AIAPIKey        string
	AIBaseURL       string
	// FallbackModels are tried when the model is overloaded.
	FallbackModels []string
	// RouterModel, from ROUTER_MODEL, is a light model that decides which
	// messages need only words; ChatModel, from CHAT_MODEL, answers those
	// without the tools, Model when empty. An empty RouterModel sends every
	// message to Model with the tools.
	RouterModel string
	ChatModel   string
	// AudioInput is true when the provider understands voice messages.
	AudioInput bool

	// Keys for the live-data tools, both optional: without FootballDataToken
	// there is no football tool, and without CoinGeckoDemoKey crypto prices
	// share CoinGecko's keyless quota.
	FootballDataToken string
	// GiphyAPIKey lets Maria send a GIF now and then instead of a sticker;
	// without it, a GIF she asks for becomes a sticker.
	GiphyAPIKey      string
	CoinGeckoDemoKey string
	// TavilyAPIKey gives Maria a web search; without it she has none.
	TavilyAPIKey string
	// TMDBToken is a TMDB read access token; without it there is no
	// film and series tool.
	TMDBToken string
	// YouTubeAPIKey is a YouTube Data API key; without it there is no
	// video search.
	YouTubeAPIKey string
	// CalendarificAPIKey is a Calendarific key; without it there is no
	// holidays tool.
	CalendarificAPIKey string
	// ImgflipAPIKey is an Imgflip API key; without it there is no meme
	// tool.
	ImgflipAPIKey string
	// URLhausAuthKey or SafeBrowsingKey lets owners turn on the link guard,
	// each service judging links when its key is set; without either it is
	// refused. SightengineUser and SightengineSecret let Sightengine judge
	// the picture guard's pictures before the bot's own model. The spam
	// guard needs no key: lols.bot answers without one, and CASKey, a
	// Combot API key, adds CAS, which answers nobody without one.
	URLhausAuthKey    string
	SafeBrowsingKey   string
	SightengineUser   string
	SightengineSecret string
	CASKey            string

	// Image generation, Gemini only. Owners have no daily limit.
	ImagesEnabled   bool
	ImageModel      string
	ImageBaseURL    string
	DailyImageLimit int

	// Media limits: the largest file the bot downloads, and the longest
	// voice message it listens to.
	MaxMediaBytes   int64
	MaxVoiceSeconds int
	Model           string
	DatabaseURL     string

	// The character. PersonaPrompt is given to the model on every request,
	// followed by GroupNote for messages from a group.
	BotDisplayName string
	PersonaPrompt  string
	GroupNote      string

	MaxHistoryMessages     int
	MaxToolCallsPerTurn    int
	MaxConcurrentRequests  int
	UserRateLimitPerMinute int
	MaxOutputTokens        int
	MaxInputChars          int
	RetentionDays          int

	RequestTimeout time.Duration
	LogLevel       slog.Level

	AdminUserIDs  map[int64]bool
	AdminCommands map[string]bool

	// Group chats: which ways of addressing the bot it reacts to.
	WakeWordTrigger bool
	MentionTrigger  bool
	ReplyTrigger    bool
	// NewHeroes adds the Mobile Legends heroes released lately, from
	// MLBBDex, to the persona at start; tests turn it off.
	NewHeroes bool
	// WakeWords are the names the bot answers to, in lower case.
	WakeWords []string

	// AmbientMessages is how many of a group's recent messages are given to
	// the model with a question. Zero turns the group log off.
	AmbientMessages int
	// Location is the groups' time zone: the times shown in the group log
	// and the turn header, the time get_current_time gives when no zone is
	// named, and where a day starts for the daily picture limit, the private
	// chats' daily limit and the owners' database tool.
	Location *time.Location
	// OwnerUsernames are the Telegram usernames of the bot's owners, lower
	// case and without @. Empty when no owner is configured.
	OwnerUsernames []string
	// OwnerIDs are the owners' numeric Telegram user IDs. When any are set,
	// only these accounts are owners and OwnerUsernames merely name them in
	// the persona: a username can be given up and taken by someone else, an
	// ID cannot.
	OwnerIDs map[int64]bool

	// AllowedBots are the usernames of other bots the bot answers in groups,
	// lower case; "*" allows any bot. Empty answers no bots.
	AllowedBots []string
	// BotRepliesPer10Min caps replies to bots per chat, so two bots cannot
	// keep talking to each other.
	BotRepliesPer10Min int
	MaxGroupReplyChars int
	// GroupRateLimitPerMinute caps answers per group across its members, and
	// MemberRepliesPer10Min answers to one member of a group, so the bot never
	// takes over a conversation.
	GroupRateLimitPerMinute int
	MemberRepliesPer10Min   int
	// PrivateDailyLimit caps the answers each user gets in private chats in
	// a day, from midnight in Location; owners have no limit and 0 turns it
	// off.
	PrivateDailyLimit int
	// PremiumPrice is what premium costs in Telegram Stars: PremiumDays days
	// of PremiumDailyLimit answers a day in private chats instead of
	// PrivateDailyLimit. 0, the default, sells none, so that a deploy never
	// starts taking money the owners did not ask for.
	PremiumPrice      int
	PremiumDays       int
	PremiumDailyLimit int
	// BusinessDailyLimit caps the answers each customer of an owner's
	// Telegram Business account gets in a day, from midnight in Location;
	// 0 turns it off.
	BusinessDailyLimit int
	// PrivateChats is whom the bot answers in private chats: "on" everyone,
	// "owners" its owners only, "off" nobody, which keeps it to groups.
	PrivateChats string
	// RequiredChannels are the channels, each its @username or -100 ID,
	// that a member must have joined to be answered in a private chat;
	// owners need not. None asks for none.
	RequiredChannels []string
	// MinGroupMembers is the smallest group answered as a group, the bot
	// counted and other bots not; a smaller one is answered only as a
	// private chat would be. 0 answers groups of any size.
	MinGroupMembers int
	// MinActiveMembers is how many people must have written in a group in
	// the last ActiveDays days for the bot to answer there; in a quieter
	// group it answers only its owners. 0 answers any group.
	MinActiveMembers int
	ActiveDays       int
}

// IsAdmin reports whether the Telegram user is listed in ADMIN_USER_IDS.
func (c Config) IsAdmin(userID int64) bool {
	return c.AdminUserIDs[userID]
}

// Load reads settings through getenv and validates them. Errors name the
// offending variable and never include its value, so secrets cannot leak.
func Load(getenv func(string) string) (Config, error) {
	r := reader{getenv: getenv}
	cfg := Config{
		TelegramToken:  r.required("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:    r.required("DB_POSTGRES_URL"),
		WebhookSecret:  r.webhookSecret("TELEGRAM_WEBHOOK_SECRET"),
		CronSecret:     r.cronSecret("CRON_SECRET"),
		TelegramAPIURL: r.text("TELEGRAM_API_URL", "https://api.telegram.org"),
		BotDisplayName: r.text("BOT_DISPLAY_NAME", "Maria"),
		GroupNote:      groupNote + "\n\n" + moderationNote,

		MaxHistoryMessages:     r.positive("MAX_HISTORY_MESSAGES", 20),
		MaxToolCallsPerTurn:    r.positive("MAX_TOOL_CALLS_PER_TURN", 4),
		MaxConcurrentRequests:  r.positive("MAX_CONCURRENT_REQUESTS", 10),
		UserRateLimitPerMinute: r.positive("USER_RATE_LIMIT_PER_MINUTE", 10),
		MaxOutputTokens:        r.positive("MAX_OUTPUT_TOKENS", 8192),
		MaxInputChars:          r.positive("MAX_INPUT_CHARS", 4000),
		RetentionDays:          r.integer("RETENTION_DAYS", 1, 0),

		RequestTimeout: time.Duration(r.positive("REQUEST_TIMEOUT_SECONDS", 45)) * time.Second,
		LogLevel:       r.logLevel("LOG_LEVEL"),

		AdminUserIDs:  r.userIDs("ADMIN_USER_IDS"),
		AdminCommands: r.commands("ADMIN_COMMANDS"),

		WakeWordTrigger: r.boolean("ENABLE_WAKE_WORD_TRIGGER", true),
		MentionTrigger:  r.boolean("ENABLE_USERNAME_MENTION", true),
		ReplyTrigger:    r.boolean("ENABLE_REPLY_TRIGGER", true),
		NewHeroes:       r.boolean("MLBB_NEW_HEROES", true),

		AmbientMessages:         r.integer("GROUP_AMBIENT_MESSAGES", 500, 0),
		MaxGroupReplyChars:      r.positive("MAX_GROUP_REPLY_LENGTH", 4000),
		GroupRateLimitPerMinute: r.positive("GROUP_RATE_LIMIT_PER_MINUTE", 6),
		MemberRepliesPer10Min:   r.positive("MEMBER_REPLIES_PER_10_MIN", 4),
		PrivateDailyLimit:       r.integer("PRIVATE_DAILY_LIMIT", 10, 0),
		PremiumPrice:            r.integer("PREMIUM_PRICE_STARS", 0, 0),
		PremiumDays:             r.positive("PREMIUM_DAYS", 30),
		PremiumDailyLimit:       r.positive("PREMIUM_DAILY_LIMIT", 50),
		BusinessDailyLimit:      r.integer("BUSINESS_DAILY_LIMIT", 30, 0),
		PrivateChats:            r.privateChats(),
		RequiredChannels:        r.requiredChannels(),
		MinGroupMembers:         r.integer("GROUP_MIN_MEMBERS", 6, 0),
		MinActiveMembers:        r.integer("GROUP_MIN_ACTIVE_MEMBERS", 0, 0),
		ActiveDays:              r.positive("GROUP_ACTIVE_DAYS", 3),
	}
	r.chooseProvider(&cfg)
	cfg.MaxMediaBytes = int64(r.positive("MAX_MEDIA_MB", 5)) << 20
	cfg.MaxVoiceSeconds = r.positive("MAX_VOICE_SECONDS", 120)
	if cfg.MaxMediaBytes > telegramDownloadLimit {
		r.fail("MAX_MEDIA_MB", "must be at most 20: Telegram does not let bots download larger files")
	}
	cfg.Location = r.location("GROUP_HISTORY_TIMEZONE", "Asia/Tashkent")
	cfg.OwnerUsernames = r.usernames("BOT_OWNER_USERNAME")
	cfg.OwnerIDs = r.ownerIDs("BOT_OWNER_IDS")
	if len(cfg.OwnerIDs) > 0 && len(cfg.OwnerUsernames) == 0 {
		r.fail("BOT_OWNER_IDS", "needs BOT_OWNER_USERNAME, which names the owners in the persona")
	}
	cfg.AllowedBots = r.allowedBots("ALLOWED_BOTS")
	cfg.BotRepliesPer10Min = r.integer("BOT_REPLIES_PER_10_MIN", 5, 0)
	cfg.PersonaPrompt = strings.ReplaceAll(r.text("PERSONA_PROMPT", defaultPersona), namePlaceholder, cfg.BotDisplayName)
	cfg.PersonaPrompt += "\n\n" + strings.ReplaceAll(modelNote, modelPlaceholder, providerLabel(cfg.AIProvider)+" ("+cfg.Model+")")
	cfg.ImagesEnabled = cfg.AIProvider == providerGemini && r.boolean("ENABLE_IMAGE_GENERATION", true)
	if cfg.AIProvider == providerGemini {
		cfg.ReasoningEffort = r.reasoningEffort("AI_REASONING_EFFORT", "low")
	}
	cfg.RouterModel = r.text("ROUTER_MODEL", "")
	cfg.ChatModel = r.text("CHAT_MODEL", cfg.Model)
	cfg.ImageModel = r.text("IMAGE_MODEL", "gemini-3.1-flash-image")
	cfg.ImageBaseURL = strings.TrimSuffix(cfg.AIBaseURL, "/openai")
	cfg.DailyImageLimit = r.integer("DAILY_IMAGE_LIMIT", 2, 0)
	if cfg.ImagesEnabled {
		cfg.PersonaPrompt += "\n\n" + imageNote
	}
	if len(cfg.OwnerUsernames) > 0 {
		cfg.PersonaPrompt += "\n\n" + strings.ReplaceAll(ownerNote, ownerPlaceholder, nameList(cfg.OwnerUsernames))
	}
	cfg.FootballDataToken = r.apiKey("FOOTBALL_DATA_TOKEN")
	cfg.GiphyAPIKey = r.apiKey("GIPHY_API_KEY")
	cfg.CoinGeckoDemoKey = r.apiKey("COINGECKO_DEMO_KEY")
	cfg.TavilyAPIKey = r.apiKey("TAVILY_API_KEY")
	cfg.TMDBToken = r.apiKey("TMDB_TOKEN")
	cfg.YouTubeAPIKey = r.apiKey("YOUTUBE_API_KEY")
	cfg.CalendarificAPIKey = r.apiKey("CALENDARIFIC_API_KEY")
	cfg.ImgflipAPIKey = r.apiKey("IMGFLIP_API_KEY")
	cfg.URLhausAuthKey = r.apiKey("URLHAUS_AUTH_KEY")
	cfg.SafeBrowsingKey = r.apiKey("GOOGLE_SAFE_BROWSING_KEY")
	cfg.SightengineUser = r.apiKey("SIGHTENGINE_API_USER")
	cfg.SightengineSecret = r.apiKey("SIGHTENGINE_API_SECRET")
	cfg.CASKey = r.apiKey("CAS_API_KEY")
	cfg.WakeWords = r.wakeWords(cfg)
	r.checkRemovedVariables()
	r.checkGroupSettings(cfg)
	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// reader collects validation errors so that Load can report all of them at once.
type reader struct {
	getenv func(string) string
	errs   []error
}

func (r *reader) fail(name, problem string) {
	r.errs = append(r.errs, fmt.Errorf("%s %s", name, problem))
}

func (r *reader) value(name string) string {
	return strings.TrimSpace(r.getenv(name))
}

func (r *reader) required(name string) string {
	value := r.value(name)
	if value == "" {
		r.fail(name, "is required")
	}
	return value
}

// reasoningEffort reads a reasoning effort: none, minimal, low, medium or
// high, or "default" for the model's own, which is sent as "".
func (r *reader) reasoningEffort(name, fallback string) string {
	switch value := strings.ToLower(r.text(name, fallback)); value {
	case "none", "minimal", "low", "medium", "high":
		return value
	case "default":
		return ""
	default:
		r.fail(name, "must be none, minimal, low, medium, high or default")
		return ""
	}
}

func (r *reader) text(name, fallback string) string {
	if value := r.value(name); value != "" {
		return value
	}
	return fallback
}

// apiKey reads an optional key that goes into a request header, where a
// space or a control character would break the request.
func (r *reader) apiKey(name string) string {
	value := r.value(name)
	if strings.IndexFunc(value, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
		r.fail(name, "must not contain spaces or control characters")
		return ""
	}
	return value
}

func (r *reader) positive(name string, fallback int) int {
	return r.integer(name, fallback, 1)
}

func (r *reader) integer(name string, fallback, minimum int) int {
	value := r.value(name)
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		r.fail(name, "must be a whole number")
		return fallback
	}
	if number < minimum {
		r.fail(name, fmt.Sprintf("must be at least %d", minimum))
		return fallback
	}
	return number
}

func (r *reader) boolean(name string, fallback bool) bool {
	switch strings.ToLower(r.value(name)) {
	case "":
		return fallback
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		r.fail(name, "must be true or false")
		return fallback
	}
}

// Model providers. Gemini is used through Google's OpenAI-compatible
// endpoint, so the same client serves both.
const (
	providerGemini = "gemini"
	providerXAI    = "xai"

	telegramDownloadLimit = 20 << 20
)

// provider describes one model provider. All are reached through the
// OpenAI-compatible chat completions API.
type provider struct {
	name, keyVar, baseURLVar, baseURL, modelVar, model string
	// fallbacks are used when FALLBACK_MODELS is not set.
	fallbacks []string
	// audio is true when the provider accepts voice messages as input.
	audio bool
	// label names the provider and its models for the persona.
	label string
}

var providers = []provider{
	{providerGemini, "GEMINI_API_KEY", "GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_MODEL", "gemini-3.6-flash",
		[]string{"gemini-3.5-flash", "gemini-flash-latest", "gemini-3.5-flash-lite"}, true, "Google's Gemini"},
	{providerXAI, "XAI_API_KEY", "XAI_BASE_URL", "https://api.x.ai/v1", "XAI_MODEL", "grok-4", nil, false, "xAI's Grok"},
}

// providerLabel is the provider's label, or its name when it has none.
func providerLabel(name string) string {
	for _, p := range providers {
		if p.name == name {
			return p.label
		}
	}
	return name
}

// chooseProvider picks the model provider by which API key is set. Exactly
// one must be: with more, it would be unclear which account is billed.
func (r *reader) chooseProvider(cfg *Config) {
	var chosen []provider
	for _, p := range providers {
		if r.value(p.keyVar) != "" {
			chosen = append(chosen, p)
		}
	}
	switch len(chosen) {
	case 0:
		if r.value("OPENROUTER_API_KEY") != "" {
			r.fail("OPENROUTER_API_KEY", "is no longer supported; set GEMINI_API_KEY instead")
			return
		}
		r.fail("GEMINI_API_KEY", "or XAI_API_KEY is required")
	case 1:
		p := chosen[0]
		cfg.AIProvider, cfg.AIAPIKey, cfg.AudioInput = p.name, r.value(p.keyVar), p.audio
		cfg.AIBaseURL = r.text(p.baseURLVar, p.baseURL)
		cfg.Model = r.text(p.modelVar, p.model)
		cfg.FallbackModels = r.fallbackModels(p.fallbacks)
	default:
		r.fail(chosen[0].keyVar, "and "+chosen[1].keyVar+" are both set; keep only one, for the provider you want to use")
	}
}

// requiredChannels reads REQUIRED_CHANNEL: channels' @usernames or -100
// IDs, separated by commas, or nothing.
func (r *reader) requiredChannels() []string {
	var channels []string
	for _, channel := range strings.Split(r.text("REQUIRED_CHANNEL", ""), ",") {
		channel = strings.TrimSpace(channel)
		if channel == "" {
			continue
		}
		if !channelName.MatchString(channel) {
			r.fail("REQUIRED_CHANNEL", "must be channels' @usernames or -100 IDs, separated by commas")
			return nil
		}
		channels = append(channels, channel)
	}
	return channels
}

var channelName = regexp.MustCompile(`^(@[A-Za-z][A-Za-z0-9_]{3,31}|-100[0-9]{5,})$`)

// privateChats reads PRIVATE_CHATS: on, owners or off.
func (r *reader) privateChats() string {
	mode := strings.ToLower(r.text("PRIVATE_CHATS", "on"))
	switch mode {
	case "on", "owners", "off":
		return mode
	}
	r.fail("PRIVATE_CHATS", "must be on, owners or off")
	return "on"
}

// wakeWords reads BOT_WAKE_WORDS, falling back to the display name. Each
// must be a single word of letters and digits, because that is what the
// wake-word matcher compares against.
func (r *reader) wakeWords(cfg Config) []string {
	words := splitList(strings.ToLower(r.value("BOT_WAKE_WORDS")))
	if len(words) == 0 {
		words = []string{strings.ToLower(cfg.BotDisplayName)}
	}
	if !cfg.WakeWordTrigger {
		return words
	}
	for _, word := range words {
		if !isSingleWord(word) {
			r.fail("BOT_WAKE_WORDS", "must be a comma-separated list of single words (letters and digits only); set it when BOT_DISPLAY_NAME is not one word")
			break
		}
	}
	return words
}

func isSingleWord(word string) bool {
	for _, char := range word {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return false
		}
	}
	return word != ""
}

// webhookSecret reads the optional secret Telegram echoes with each webhook
// request. Telegram accepts 1 to 256 characters from A-Z, a-z, 0-9, _ and -.
func (r *reader) webhookSecret(name string) string {
	secret := r.value(name)
	if len(secret) > maxWebhookSecretLength || strings.ContainsFunc(secret, notSecretChar) {
		r.fail(name, "must be at most 256 characters from A-Z, a-z, 0-9, _ and -")
		return ""
	}
	return secret
}

// cronSecret reads the optional secret for scheduled calls. It guards an
// endpoint that deletes history, so it must not be easy to guess.
func (r *reader) cronSecret(name string) string {
	secret := r.value(name)
	if secret != "" && len(secret) < minCronSecretLength {
		r.fail(name, fmt.Sprintf("must be at least %d characters", minCronSecretLength))
		return ""
	}
	return secret
}

func notSecretChar(char rune) bool {
	isLetter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
	isDigit := char >= '0' && char <= '9'
	return !isLetter && !isDigit && char != '_' && char != '-'
}

// fallbackModels reads FALLBACK_MODELS: a comma-separated list, "none" for
// no fallbacks, or empty for the provider's defaults.
func (r *reader) fallbackModels(defaults []string) []string {
	switch value := r.value("FALLBACK_MODELS"); value {
	case "":
		return defaults
	case "none":
		return nil
	default:
		return splitList(value)
	}
}

// usernames reads a comma-separated list of Telegram usernames, with or
// without @, and returns them in lower case without @.
func (r *reader) usernames(name string) []string {
	var names []string
	for _, item := range splitList(r.value(name)) {
		username := strings.ToLower(strings.TrimPrefix(item, "@"))
		if !isUsername(username) {
			r.fail(name, "must be a comma-separated list of Telegram usernames")
			return nil
		}
		names = append(names, username)
	}
	return names
}

func (r *reader) allowedBots(name string) []string {
	if r.value(name) == "*" {
		return []string{"*"}
	}
	return r.usernames(name)
}

// isUsername accepts what Telegram allows: letters, digits and underscores.
func isUsername(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && char != '_' {
			return false
		}
	}
	return true
}

// nameList writes usernames as "a" or "a and b", without "@": written
// with it, the model tagged the owners in groups they are not in.
func nameList(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func (r *reader) location(name, fallback string) *time.Location {
	location, err := time.LoadLocation(r.text(name, fallback))
	if err != nil {
		r.fail(name, "must be an IANA time zone such as Asia/Tashkent")
		return time.UTC
	}
	return location
}

func (r *reader) checkRemovedVariables() {
	for name, replacement := range removedVariables {
		if r.value(name) != "" {
			r.fail(name, "is no longer supported; use "+replacement)
		}
	}
}

// checkGroupSettings validates rules that involve a fixed range or set of
// values.
func (r *reader) checkGroupSettings(cfg Config) {
	if cfg.MaxGroupReplyChars > telegramMessageLength {
		r.fail("MAX_GROUP_REPLY_LENGTH", fmt.Sprintf("must be at most %d", telegramMessageLength))
	}
	if cfg.AmbientMessages > maxAmbientMessages {
		r.fail("GROUP_AMBIENT_MESSAGES", fmt.Sprintf("must be at most %d", maxAmbientMessages))
	}
	if mode := r.text("GROUP_CONTEXT_MODE", groupContextShared); mode != groupContextShared {
		r.fail("GROUP_CONTEXT_MODE", "must be shared; each group has one conversation with the bot")
	}
}

func (r *reader) logLevel(name string) slog.Level {
	levels := map[string]slog.Level{
		"":      slog.LevelInfo,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	level, ok := levels[strings.ToLower(r.value(name))]
	if !ok {
		r.fail(name, "must be one of debug, info, warn, error")
	}
	return level
}

func (r *reader) userIDs(name string) map[int64]bool {
	ids := map[int64]bool{}
	for _, item := range splitList(r.value(name)) {
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil {
			r.fail(name, "must be a comma-separated list of numeric Telegram user IDs")
			return ids
		}
		ids[id] = true
	}
	return ids
}

// ownerIDs reads the owners' account IDs. Once set they are the only way an
// owner is recognised, so an entry no user can have, such as a group's
// negative chat ID pasted by mistake, would quietly leave the bot without
// owners; it is refused instead.
func (r *reader) ownerIDs(name string) map[int64]bool {
	ids := r.userIDs(name)
	for id := range ids {
		if id <= 0 {
			r.fail(name, "must be positive Telegram user IDs, not chat IDs")
			break
		}
	}
	return ids
}

func (r *reader) commands(name string) map[string]bool {
	commands := map[string]bool{}
	for _, item := range splitList(r.value(name)) {
		commands["/"+strings.ToLower(strings.TrimPrefix(item, "/"))] = true
	}
	return commands
}

func splitList(value string) []string {
	var items []string
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
