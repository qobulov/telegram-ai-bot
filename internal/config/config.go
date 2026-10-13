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
	groupContextShared    = "shared"
	telegramMessageLength = 4096
	maxAmbientMessages    = 100
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
	TelegramToken     string
	TelegramAPIURL    string
	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	Model             string
	DatabaseURL       string

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
	AmbientMessages         int
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
		TelegramToken:     r.required("TELEGRAM_BOT_TOKEN"),
		OpenRouterAPIKey:  r.required("OPENROUTER_API_KEY"),
		DatabaseURL:       r.required("db_POSTGRES_URL"),
		TelegramAPIURL:    r.text("TELEGRAM_API_URL", "https://api.telegram.org"),
		OpenRouterBaseURL: r.text("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		Model:             r.text("OPENROUTER_MODEL", "openrouter/free"),
		BotDisplayName:    r.text("BOT_DISPLAY_NAME", "Mira"),
		GroupNote:         groupNote,

		MaxHistoryMessages:     r.positive("MAX_HISTORY_MESSAGES", 20),
		MaxToolCallsPerTurn:    r.positive("MAX_TOOL_CALLS_PER_TURN", 4),
		MaxConcurrentRequests:  r.positive("MAX_CONCURRENT_REQUESTS", 10),
		UserRateLimitPerMinute: r.positive("USER_RATE_LIMIT_PER_MINUTE", 10),
		MaxOutputTokens:        r.positive("MAX_OUTPUT_TOKENS", 1024),
		MaxInputChars:          r.positive("MAX_INPUT_CHARS", 4000),
		RetentionDays:          r.integer("RETENTION_DAYS", 90, 0),

		RequestTimeout: time.Duration(r.positive("REQUEST_TIMEOUT_SECONDS", 45)) * time.Second,
		LogLevel:       r.logLevel("LOG_LEVEL"),

		AdminUserIDs:  r.userIDs("ADMIN_USER_IDS"),
		AdminCommands: r.commands("ADMIN_COMMANDS"),

		WakeWordTrigger: r.boolean("ENABLE_WAKE_WORD_TRIGGER", true),
		MentionTrigger:  r.boolean("ENABLE_USERNAME_MENTION", true),
		ReplyTrigger:    r.boolean("ENABLE_REPLY_TRIGGER", true),

		AmbientMessages:         r.integer("GROUP_AMBIENT_MESSAGES", 20, 0),
		MaxGroupReplyChars:      r.positive("MAX_GROUP_REPLY_LENGTH", 4000),
		GroupRateLimitPerMinute: r.positive("GROUP_RATE_LIMIT_PER_MINUTE", 10),
	}
	cfg.PersonaPrompt = strings.ReplaceAll(r.text("PERSONA_PROMPT", defaultPersona), namePlaceholder, cfg.BotDisplayName)
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
