// Package config loads and validates the bot's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
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
	// The model provider. Both Gemini and OpenRouter are reached through the
	// same OpenAI-compatible chat completions API; only these differ.
	AIProvider string
	AIAPIKey   string
	AIBaseURL  string
	// FallbackModels are tried when the model is overloaded.
	FallbackModels []string
	// AudioInput is true when the provider understands voice messages.
	AudioInput bool
	// The backup provider, asked when every model of the main one failed.
	// Empty BackupAPIKey means none.
	BackupAPIKey  string
	BackupBaseURL string
	BackupModel   string

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
	// WakeWords are the names the bot answers to, in lower case.
	WakeWords []string

	// AmbientMessages is how many recent ordinary group messages are kept in
	// memory as context. Zero disables it.
	AmbientMessages int
	// Location is the time zone that decides when "today" starts for the
	// group history.
	Location *time.Location
	// OwnerUsernames are the Telegram usernames of the bot's owners, lower
	// case and without @. Empty when no owner is configured.
	OwnerUsernames []string

	// AllowedBots are the usernames of other bots the bot answers in groups,
	// lower case; "*" allows any bot. Empty answers no bots.
	AllowedBots []string
	// BotRepliesPer10Min caps replies to bots per chat, so two bots cannot
	// keep talking to each other.
	BotRepliesPer10Min      int
	MaxGroupReplyChars      int
	GroupRateLimitPerMinute int
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
		GroupNote:      groupNote,

		MaxHistoryMessages:     r.positive("MAX_HISTORY_MESSAGES", 20),
		MaxToolCallsPerTurn:    r.positive("MAX_TOOL_CALLS_PER_TURN", 4),
		MaxConcurrentRequests:  r.positive("MAX_CONCURRENT_REQUESTS", 10),
		UserRateLimitPerMinute: r.positive("USER_RATE_LIMIT_PER_MINUTE", 10),
		MaxOutputTokens:        r.positive("MAX_OUTPUT_TOKENS", 1024),
		MaxInputChars:          r.positive("MAX_INPUT_CHARS", 4000),
		RetentionDays:          r.integer("RETENTION_DAYS", 1, 0),

		RequestTimeout: time.Duration(r.positive("REQUEST_TIMEOUT_SECONDS", 45)) * time.Second,
		LogLevel:       r.logLevel("LOG_LEVEL"),

		AdminUserIDs:  r.userIDs("ADMIN_USER_IDS"),
		AdminCommands: r.commands("ADMIN_COMMANDS"),

		WakeWordTrigger: r.boolean("ENABLE_WAKE_WORD_TRIGGER", true),
		MentionTrigger:  r.boolean("ENABLE_USERNAME_MENTION", true),
		ReplyTrigger:    r.boolean("ENABLE_REPLY_TRIGGER", true),

		AmbientMessages:         r.integer("GROUP_AMBIENT_MESSAGES", 200, 0),
		MaxGroupReplyChars:      r.positive("MAX_GROUP_REPLY_LENGTH", 4000),
		GroupRateLimitPerMinute: r.positive("GROUP_RATE_LIMIT_PER_MINUTE", 10),
	}
	r.chooseProvider(&cfg)
	cfg.MaxMediaBytes = int64(r.positive("MAX_MEDIA_MB", 5)) << 20
	cfg.MaxVoiceSeconds = r.positive("MAX_VOICE_SECONDS", 120)
	if cfg.MaxMediaBytes > telegramDownloadLimit {
		r.fail("MAX_MEDIA_MB", "must be at most 20: Telegram does not let bots download larger files")
	}
	cfg.Location = r.location("GROUP_HISTORY_TIMEZONE", "Asia/Tashkent")
	cfg.OwnerUsernames = r.usernames("BOT_OWNER_USERNAME")
	cfg.AllowedBots = r.allowedBots("ALLOWED_BOTS")
	cfg.BotRepliesPer10Min = r.integer("BOT_REPLIES_PER_10_MIN", 5, 0)
	cfg.PersonaPrompt = strings.ReplaceAll(r.text("PERSONA_PROMPT", defaultPersona), namePlaceholder, cfg.BotDisplayName)
	cfg.ImagesEnabled = cfg.AIProvider == providerGemini && r.boolean("ENABLE_IMAGE_GENERATION", true)
	cfg.ImageModel = r.text("IMAGE_MODEL", "gemini-3.1-flash-image")
	cfg.ImageBaseURL = strings.TrimSuffix(cfg.AIBaseURL, "/openai")
	cfg.DailyImageLimit = r.integer("DAILY_IMAGE_LIMIT", 2, 0)
	if cfg.ImagesEnabled {
		cfg.PersonaPrompt += "\n\n" + imageNote
	}
	if len(cfg.OwnerUsernames) > 0 {
		cfg.PersonaPrompt += "\n\n" + strings.ReplaceAll(ownerNote, ownerPlaceholder, mentionList(cfg.OwnerUsernames))
	}
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

func (r *reader) text(name, fallback string) string {
	if value := r.value(name); value != "" {
		return value
	}
	return fallback
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
	providerGemini     = "gemini"
	providerOpenRouter = "openrouter"
	providerXAI        = "xai"

	telegramDownloadLimit = 20 << 20

	// The backup provider defaults to Groq, which has a free tier.
	defaultBackupBaseURL = "https://api.groq.com/openai/v1"
	defaultBackupModel   = "openai/gpt-oss-120b"
)

// provider describes one model provider. All are reached through the
// OpenAI-compatible chat completions API.
type provider struct {
	name, keyVar, baseURLVar, baseURL, modelVar, model string
	// fallbacks are used when FALLBACK_MODELS is not set.
	fallbacks []string
	// audio is true when the provider accepts voice messages as input.
	audio bool
}

var providers = []provider{
	{providerGemini, "GEMINI_API_KEY", "GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_MODEL", "gemini-3.5-flash-lite",
		[]string{"gemini-3.1-flash-lite", "gemini-flash-lite-latest", "gemini-flash-latest"}, true},
	{providerXAI, "XAI_API_KEY", "XAI_BASE_URL", "https://api.x.ai/v1", "XAI_MODEL", "grok-4", nil, false},
	{providerOpenRouter, "OPENROUTER_API_KEY", "OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1", "OPENROUTER_MODEL", "openrouter/free", nil, false},
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
		r.fail("GEMINI_API_KEY", "or XAI_API_KEY or OPENROUTER_API_KEY is required")
	case 1:
		p := chosen[0]
		cfg.AIProvider, cfg.AIAPIKey, cfg.AudioInput = p.name, r.value(p.keyVar), p.audio
		cfg.AIBaseURL = r.text(p.baseURLVar, p.baseURL)
		cfg.Model = r.text(p.modelVar, p.model)
		cfg.FallbackModels = r.fallbackModels(p.fallbacks)
		cfg.BackupAPIKey = r.value("BACKUP_API_KEY")
		cfg.BackupBaseURL = r.text("BACKUP_BASE_URL", defaultBackupBaseURL)
		cfg.BackupModel = r.text("BACKUP_MODEL", defaultBackupModel)
	default:
		r.fail(chosen[0].keyVar, "and "+chosen[1].keyVar+" are both set; keep only one, for the provider you want to use")
	}
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

// mentionList writes usernames as "@a" or "@a and @b".
func mentionList(names []string) string {
	mentions := make([]string, len(names))
	for i, name := range names {
		mentions[i] = "@" + name
	}
	if len(mentions) == 1 {
		return mentions[0]
	}
	return strings.Join(mentions[:len(mentions)-1], ", ") + " and " + mentions[len(mentions)-1]
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
