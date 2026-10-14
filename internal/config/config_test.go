package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testToken = "123456:secret-bot-token"
	testKey   = "sk-or-secret-key"
	testDBURL = "postgres://bot:secret-db-pass@localhost/bot"
)

func validEnv() map[string]string {
	return map[string]string{
		"TELEGRAM_BOT_TOKEN": testToken,
		"OPENROUTER_API_KEY": testKey,
		"DB_POSTGRES_URL":    testDBURL,
	}
}

func getenv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "openrouter/free" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.AIBaseURL != "https://openrouter.ai/api/v1" || cfg.AIProvider != "openrouter" || cfg.AIAPIKey != testKey {
		t.Errorf("AI settings = %q, %q", cfg.AIProvider, cfg.AIBaseURL)
	}
	if cfg.TelegramAPIURL != "https://api.telegram.org" {
		t.Errorf("TelegramAPIURL = %q", cfg.TelegramAPIURL)
	}
	if cfg.PersonaPrompt == "" {
		t.Error("PersonaPrompt is empty")
	}
	wantInts := map[string][2]int{
		"MaxHistoryMessages":     {cfg.MaxHistoryMessages, 20},
		"MaxToolCallsPerTurn":    {cfg.MaxToolCallsPerTurn, 4},
		"MaxConcurrentRequests":  {cfg.MaxConcurrentRequests, 10},
		"UserRateLimitPerMinute": {cfg.UserRateLimitPerMinute, 10},
		"MaxOutputTokens":        {cfg.MaxOutputTokens, 1024},
		"MaxInputChars":          {cfg.MaxInputChars, 4000},
		"RetentionDays":          {cfg.RetentionDays, 1},
	}
	for name, pair := range wantInts {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}
	if cfg.RequestTimeout != 45*time.Second {
		t.Errorf("RequestTimeout = %v", cfg.RequestTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	for _, name := range []string{"TELEGRAM_BOT_TOKEN", "OPENROUTER_API_KEY", "DB_POSTGRES_URL"} {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			delete(env, name)
			_, err := Load(getenv(env))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("err = %v, want mention of %s", err, name)
			}
		})
	}
}

func TestLoadRejectsBadNumbers(t *testing.T) {
	for _, value := range []string{"abc", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			env := validEnv()
			env["MAX_HISTORY_MESSAGES"] = value
			_, err := Load(getenv(env))
			if err == nil || !strings.Contains(err.Error(), "MAX_HISTORY_MESSAGES") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLoadAllowsZeroRetention(t *testing.T) {
	env := validEnv()
	env["RETENTION_DAYS"] = "0"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RetentionDays != 0 {
		t.Errorf("RetentionDays = %d", cfg.RetentionDays)
	}

	env["RETENTION_DAYS"] = "-5"
	if _, err := Load(getenv(env)); err == nil {
		t.Error("negative RETENTION_DAYS accepted")
	}
}

func TestLoadParsesAdmins(t *testing.T) {
	env := validEnv()
	env["ADMIN_USER_IDS"] = "1, 2"
	env["ADMIN_COMMANDS"] = "model, /reset"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsAdmin(1) || !cfg.IsAdmin(2) || cfg.IsAdmin(3) {
		t.Errorf("AdminUserIDs = %v", cfg.AdminUserIDs)
	}
	if !cfg.AdminCommands["/model"] || !cfg.AdminCommands["/reset"] || len(cfg.AdminCommands) != 2 {
		t.Errorf("AdminCommands = %v", cfg.AdminCommands)
	}
}

func TestLoadRejectsBadAdminID(t *testing.T) {
	env := validEnv()
	env["ADMIN_USER_IDS"] = "1,x"
	_, err := Load(getenv(env))
	if err == nil || !strings.Contains(err.Error(), "ADMIN_USER_IDS") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsBadLogLevel(t *testing.T) {
	env := validEnv()
	env["LOG_LEVEL"] = "loud"
	_, err := Load(getenv(env))
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadErrorsNeverContainSecrets(t *testing.T) {
	env := validEnv()
	env["MAX_HISTORY_MESSAGES"] = "abc"
	env["LOG_LEVEL"] = "loud"
	env["ADMIN_USER_IDS"] = "x"
	_, err := Load(getenv(env))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{testToken, testKey, testDBURL, "secret-db-pass"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks secret %q: %v", secret, err)
		}
	}
}

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nDOTENV_PLAIN=value\nDOTENV_QUOTED=\"quoted value\"\nexport DOTENV_EXPORTED='single'\nDOTENV_EXISTING=from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTENV_EXISTING", "from-env")
	for _, key := range []string{"DOTENV_PLAIN", "DOTENV_QUOTED", "DOTENV_EXPORTED"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	want := map[string]string{
		"DOTENV_PLAIN":    "value",
		"DOTENV_QUOTED":   "quoted value",
		"DOTENV_EXPORTED": "single",
		"DOTENV_EXISTING": "from-env",
	}
	for key, value := range want {
		if got := os.Getenv(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestLoadDotEnvMissingFileIsFine(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
}

func TestLoadDotEnvRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("NO_EQUALS_SIGN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := LoadDotEnv(path)
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadCharacterDefaults(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WakeWordTrigger || !cfg.MentionTrigger || !cfg.ReplyTrigger {
		t.Errorf("triggers = wake %v, mention %v, reply %v", cfg.WakeWordTrigger, cfg.MentionTrigger, cfg.ReplyTrigger)
	}
	if cfg.BotDisplayName != "Maria" || len(cfg.WakeWords) != 1 || cfg.WakeWords[0] != "maria" {
		t.Errorf("name = %q, wake words = %q", cfg.BotDisplayName, cfg.WakeWords)
	}
	if cfg.AmbientMessages != 200 || cfg.MaxGroupReplyChars != 4000 || cfg.GroupRateLimitPerMinute != 10 {
		t.Errorf("cfg = %d, %d, %d", cfg.AmbientMessages, cfg.MaxGroupReplyChars, cfg.GroupRateLimitPerMinute)
	}
}

func TestBuiltInPersona(t *testing.T) {
	env := validEnv()
	env["BOT_DISPLAY_NAME"] = "Zara"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.PersonaPrompt, "You are Zara") || strings.Contains(cfg.PersonaPrompt, "{name}") {
		t.Errorf("the name was not put into the persona: %.80q", cfg.PersonaPrompt)
	}
	if strings.Contains(cfg.PersonaPrompt, "Mira") {
		t.Error("the built-in persona names the character directly instead of using the configured name")
	}
	for _, want := range []string{"slur", "untrusted", "threat", "never claim to be a real human", "calculator", "plain text"} {
		if !strings.Contains(strings.ToLower(cfg.PersonaPrompt), want) {
			t.Errorf("the built-in persona does not mention %q", want)
		}
	}
	if !strings.Contains(cfg.GroupNote, "[silent]") {
		t.Errorf("the group note does not explain the silent marker: %q", cfg.GroupNote)
	}
	if len(cfg.WakeWords) != 1 || cfg.WakeWords[0] != "zara" {
		t.Errorf("wake words = %q, want the display name", cfg.WakeWords)
	}
}

func TestCustomPersona(t *testing.T) {
	env := validEnv()
	env["PERSONA_PROMPT"] = "You are {name}, a grumpy cat. {name} hates Mondays."
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PersonaPrompt != "You are Maria, a grumpy cat. Maria hates Mondays." {
		t.Errorf("PersonaPrompt = %q", cfg.PersonaPrompt)
	}
}

func TestLoadWakeWords(t *testing.T) {
	env := validEnv()
	env["BOT_WAKE_WORDS"] = "Mira, мира ,MIRKA"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(cfg.WakeWords, "|"); got != "mira|мира|mirka" {
		t.Errorf("wake words = %q", got)
	}

	for _, bad := range []string{"hey mira", "mira!", "@mira"} {
		env["BOT_WAKE_WORDS"] = bad
		if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "BOT_WAKE_WORDS") {
			t.Errorf("%q: err = %v, want mention of BOT_WAKE_WORDS", bad, err)
		}
	}
}

func TestDisplayNameMustWorkAsWakeWordOrBeOverridden(t *testing.T) {
	env := validEnv()
	env["BOT_DISPLAY_NAME"] = "Mira the Great"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "BOT_WAKE_WORDS") {
		t.Fatalf("err = %v, want a request to set BOT_WAKE_WORDS", err)
	}

	env["BOT_WAKE_WORDS"] = "mira"
	if _, err := Load(getenv(env)); err != nil {
		t.Errorf("a multi-word name with explicit wake words was rejected: %v", err)
	}

	delete(env, "BOT_WAKE_WORDS")
	env["ENABLE_WAKE_WORD_TRIGGER"] = "false"
	if _, err := Load(getenv(env)); err != nil {
		t.Errorf("a multi-word name with the wake-word trigger off was rejected: %v", err)
	}
}

func TestLoadParsesBooleans(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "TRUE": true, "1": true, "false": false, "False": false, "0": false} {
		env := validEnv()
		env["ENABLE_USERNAME_MENTION"] = value
		cfg, err := Load(getenv(env))
		if err != nil {
			t.Fatalf("%q: Load: %v", value, err)
		}
		if cfg.MentionTrigger != want {
			t.Errorf("%q parsed as %v", value, cfg.MentionTrigger)
		}
	}

	env := validEnv()
	env["ENABLE_REPLY_TRIGGER"] = "yes"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "ENABLE_REPLY_TRIGGER") {
		t.Errorf("err = %v, want mention of ENABLE_REPLY_TRIGGER", err)
	}
}

func TestLoadGroupContextMode(t *testing.T) {
	for value, ok := range map[string]bool{"": true, "shared": true, "per_user": false, "bogus": false} {
		env := validEnv()
		env["GROUP_CONTEXT_MODE"] = value
		_, err := Load(getenv(env))
		if ok && err != nil {
			t.Errorf("%q rejected: %v", value, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "GROUP_CONTEXT_MODE")) {
			t.Errorf("%q: err = %v, want mention of GROUP_CONTEXT_MODE", value, err)
		}
	}
}

func TestRemovedVariablesAreReported(t *testing.T) {
	removed := map[string]string{
		"SYSTEM_PROMPT":               "PERSONA_PROMPT",
		"GROUP_SYSTEM_PROMPT":         "PERSONA_PROMPT",
		"ENABLE_MENTION_TRIGGER":      "ENABLE_USERNAME_MENTION",
		"ENABLE_DISPLAY_NAME_TRIGGER": "ENABLE_WAKE_WORD_TRIGGER",
	}
	for name, replacement := range removed {
		env := validEnv()
		env[name] = "true"
		_, err := Load(getenv(env))
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), replacement) {
			t.Errorf("%s: err = %v, want it to name the variable and %s", name, err, replacement)
		}
	}
}

func TestLoadAmbientMessages(t *testing.T) {
	for value, ok := range map[string]bool{"0": true, "500": true, "-1": false, "501": false} {
		env := validEnv()
		env["GROUP_AMBIENT_MESSAGES"] = value
		_, err := Load(getenv(env))
		if ok != (err == nil) {
			t.Errorf("GROUP_AMBIENT_MESSAGES=%s: err = %v, want ok=%v", value, err, ok)
		}
	}
}

func TestLoadGroupLimits(t *testing.T) {
	cases := map[string]map[string]bool{
		"MAX_GROUP_REPLY_LENGTH":      {"0": false, "4097": false, "4096": true, "1": true},
		"GROUP_RATE_LIMIT_PER_MINUTE": {"0": false, "-1": false, "5": true},
	}
	for name, values := range cases {
		for value, ok := range values {
			env := validEnv()
			env[name] = value
			_, err := Load(getenv(env))
			if ok != (err == nil) {
				t.Errorf("%s=%s: err = %v, want ok=%v", name, value, err, ok)
			}
		}
	}
}

func TestWebhookSecret(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil || cfg.WebhookSecret != "" {
		t.Fatalf("default: secret = %q, err = %v; the secret is optional for polling", cfg.WebhookSecret, err)
	}

	env := validEnv()
	env["TELEGRAM_WEBHOOK_SECRET"] = "Abc_123-xyz"
	if cfg, err := Load(getenv(env)); err != nil || cfg.WebhookSecret != "Abc_123-xyz" {
		t.Errorf("secret = %q, err = %v", cfg.WebhookSecret, err)
	}

	for _, bad := range []string{"has space", "semi;colon", "ünïcode", strings.Repeat("a", 257)} {
		env["TELEGRAM_WEBHOOK_SECRET"] = bad
		_, err := Load(getenv(env))
		if err == nil || !strings.Contains(err.Error(), "TELEGRAM_WEBHOOK_SECRET") {
			t.Errorf("%q: err = %v, want mention of TELEGRAM_WEBHOOK_SECRET", bad, err)
		}
		if err != nil && strings.Contains(err.Error(), bad) {
			t.Errorf("the error repeats the secret %q", bad)
		}
	}
}

func geminiEnv() map[string]string {
	env := validEnv()
	delete(env, "OPENROUTER_API_KEY")
	env["GEMINI_API_KEY"] = "AIza-gemini-secret"
	return env
}

func TestGeminiKeySelectsGemini(t *testing.T) {
	cfg, err := Load(getenv(geminiEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIProvider != "gemini" || cfg.AIAPIKey != "AIza-gemini-secret" {
		t.Errorf("provider = %q", cfg.AIProvider)
	}
	if cfg.AIBaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("base URL = %q, want Gemini's OpenAI-compatible endpoint", cfg.AIBaseURL)
	}
	if cfg.Model != "gemini-3.5-flash-lite" {
		t.Errorf("model = %q", cfg.Model)
	}
}

func TestGeminiModelCanBeChosen(t *testing.T) {
	env := geminiEnv()
	env["GEMINI_MODEL"] = "gemini-2.5-pro"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "gemini-2.5-pro" {
		t.Errorf("model = %q", cfg.Model)
	}
}

func TestOpenRouterSettingsDoNotLeakIntoGemini(t *testing.T) {
	env := geminiEnv()
	env["OPENROUTER_MODEL"] = "openrouter/free"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "gemini-3.5-flash-lite" {
		t.Errorf("model = %q; an OpenRouter model name must not be sent to Gemini", cfg.Model)
	}
}

func TestOneAIKeyIsRequired(t *testing.T) {
	env := validEnv()
	delete(env, "OPENROUTER_API_KEY")
	_, err := Load(getenv(env))
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Errorf("no key: err = %v, want it to name both variables", err)
	}

	both := geminiEnv()
	both["OPENROUTER_API_KEY"] = testKey
	_, err = Load(getenv(both))
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Errorf("both keys: err = %v, want a request to keep only one", err)
	}
	if err != nil && (strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "AIza-gemini-secret")) {
		t.Errorf("the error repeats a key: %v", err)
	}
}

func TestXAIKeySelectsGrok(t *testing.T) {
	env := validEnv()
	delete(env, "OPENROUTER_API_KEY")
	env["XAI_API_KEY"] = "xai-secret"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIProvider != "xai" || cfg.AIAPIKey != "xai-secret" || cfg.AIBaseURL != "https://api.x.ai/v1" || cfg.Model != "grok-4" {
		t.Errorf("provider = %q, base = %q, model = %q", cfg.AIProvider, cfg.AIBaseURL, cfg.Model)
	}
	if cfg.AudioInput {
		t.Error("audio input is enabled for Grok; only Gemini understands voice messages here")
	}

	env["XAI_MODEL"] = "grok-4-fast"
	if cfg, _ := Load(getenv(env)); cfg.Model != "grok-4-fast" {
		t.Errorf("model = %q", cfg.Model)
	}
}

func TestOnlyGeminiHearsVoice(t *testing.T) {
	cfg, err := Load(getenv(geminiEnv()))
	if err != nil || !cfg.AudioInput {
		t.Errorf("Gemini: audio input = %v, err = %v", cfg.AudioInput, err)
	}
	cfg, err = Load(getenv(validEnv()))
	if err != nil || cfg.AudioInput {
		t.Errorf("OpenRouter: audio input = %v, err = %v", cfg.AudioInput, err)
	}
}

func TestExactlyOneOfThreeKeys(t *testing.T) {
	env := geminiEnv()
	env["XAI_API_KEY"] = "xai-secret"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "only one") {
		t.Errorf("Gemini and xAI keys: err = %v", err)
	}
	none := validEnv()
	delete(none, "OPENROUTER_API_KEY")
	if _, err := Load(getenv(none)); err == nil || !strings.Contains(err.Error(), "XAI_API_KEY") {
		t.Errorf("no key: err = %v, want it to mention XAI_API_KEY too", err)
	}
}

func TestMediaLimits(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxMediaBytes != 5<<20 || cfg.MaxVoiceSeconds != 120 {
		t.Errorf("defaults = %d bytes, %d s", cfg.MaxMediaBytes, cfg.MaxVoiceSeconds)
	}
	env := validEnv()
	env["MAX_MEDIA_MB"] = "25"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "MAX_MEDIA_MB") {
		t.Errorf("MAX_MEDIA_MB=25: err = %v; Telegram lets bots download at most 20 MB", err)
	}
	env["MAX_MEDIA_MB"] = "2"
	env["MAX_VOICE_SECONDS"] = "30"
	if cfg, err := Load(getenv(env)); err != nil || cfg.MaxMediaBytes != 2<<20 || cfg.MaxVoiceSeconds != 30 {
		t.Errorf("custom limits = %d, %d, err = %v", cfg.MaxMediaBytes, cfg.MaxVoiceSeconds, err)
	}
}

func TestCronSecret(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil || cfg.CronSecret != "" {
		t.Fatalf("default: %q, %v", cfg.CronSecret, err)
	}
	env := validEnv()
	env["CRON_SECRET"] = "a-long-random-cron-secret"
	if cfg, err := Load(getenv(env)); err != nil || cfg.CronSecret != "a-long-random-cron-secret" {
		t.Errorf("CronSecret = %q, err = %v", cfg.CronSecret, err)
	}
	env["CRON_SECRET"] = "short"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "CRON_SECRET") || strings.Contains(err.Error(), "short") {
		t.Errorf("short secret: err = %v, want it rejected without repeating it", err)
	}
}

func TestGeminiFallbackModels(t *testing.T) {
	cfg, err := Load(getenv(geminiEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.FallbackModels, ","); got != "gemini-3.1-flash-lite,gemini-flash-lite-latest" {
		t.Errorf("default Gemini fallbacks = %q", got)
	}

	env := geminiEnv()
	env["FALLBACK_MODELS"] = "gemini-3.6-flash, gemini-3.5-flash"
	if cfg, _ := Load(getenv(env)); strings.Join(cfg.FallbackModels, ",") != "gemini-3.6-flash,gemini-3.5-flash" {
		t.Errorf("custom fallbacks = %q", cfg.FallbackModels)
	}

	env["FALLBACK_MODELS"] = "none"
	if cfg, _ := Load(getenv(env)); len(cfg.FallbackModels) != 0 {
		t.Errorf("FALLBACK_MODELS=none gave %q", cfg.FallbackModels)
	}

	if cfg, _ := Load(getenv(validEnv())); len(cfg.FallbackModels) != 0 {
		t.Errorf("OpenRouter got default fallbacks %q; they are Gemini model names", cfg.FallbackModels)
	}
}

func TestBuiltInPersonaPutsMoodBeforeJokes(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	persona := cfg.PersonaPrompt
	mood := strings.Index(persona, "# Read the mood first")
	humor := strings.Index(persona, "# Humor and Roasting")
	if mood < 0 || humor < 0 || mood > humor {
		t.Errorf("the mood rule must come before the humor section (mood at %d, humor at %d)", mood, humor)
	}
	// Small models copy examples. No example may joke at someone who is down.
	if strings.Contains(persona, "beta-test") {
		t.Error("the persona still has the example that mocks a person having a terrible day")
	}
	for _, want := range []string{"kayfiyatim yo'q", "roast me"} {
		if !strings.Contains(persona, want) {
			t.Errorf("the persona has no example for %q", want)
		}
	}
}

func TestGroupNoteReservesSilenceForMessagesNotMeantForTheBot(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.GroupNote, "talking about you rather than to you") || !strings.Contains(cfg.GroupNote, "fire back") {
		t.Errorf("group note = %q; it must say when to stay silent and that jabs get an answer", cfg.GroupNote)
	}
}

func TestGroupHistoryTimezone(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Location == nil || cfg.Location.String() != "Asia/Tashkent" {
		t.Errorf("Location = %v, want Asia/Tashkent", cfg.Location)
	}
	env := validEnv()
	env["GROUP_HISTORY_TIMEZONE"] = "Mars/Olympus"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "GROUP_HISTORY_TIMEZONE") {
		t.Errorf("err = %v", err)
	}
}

func TestBotOwner(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "@Pronium"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OwnerUsernames) != 1 || cfg.OwnerUsernames[0] != "pronium" {
		t.Errorf("OwnerUsernames = %q, want it without @ and in lower case", cfg.OwnerUsernames)
	}
	if !strings.Contains(cfg.PersonaPrompt, "@pronium") || !strings.Contains(cfg.PersonaPrompt, "[owner]") {
		t.Error("the persona does not tell the model who its owner is and how owner messages are marked")
	}

	env["PERSONA_PROMPT"] = "You are {name}."
	cfg, _ = Load(getenv(env))
	if !strings.Contains(cfg.PersonaPrompt, "@pronium") {
		t.Error("a custom persona lost the owner note")
	}

	cfg, _ = Load(getenv(validEnv()))
	if strings.Contains(cfg.PersonaPrompt, "[owner]") {
		t.Error("an owner note was added although no owner is configured")
	}
}

func TestPersonaUsesEmojiSparingly(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.PersonaPrompt, "Most of your messages have no emoji") {
		t.Error("the persona does not limit emoji")
	}
	if n := strings.Count(cfg.PersonaPrompt, "😂"); n > 1 {
		t.Errorf("the persona's examples use 😂 %d times; examples teach the habit", n)
	}
}

func TestPersonaDoesNotClaimExperiences(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.PersonaPrompt, "never claim you did") {
		t.Error("the persona does not stop the model inventing things it saw or did")
	}
}

func TestTwoOwners(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "pronium, @TursunoffM"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.OwnerUsernames, ",") != "pronium,tursunoffm" {
		t.Errorf("OwnerUsernames = %q", cfg.OwnerUsernames)
	}
	if !strings.Contains(cfg.PersonaPrompt, "@pronium") || !strings.Contains(cfg.PersonaPrompt, "@tursunoffm") {
		t.Error("the persona does not name both owners")
	}

	env["BOT_OWNER_USERNAME"] = "pronium, not a username"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "BOT_OWNER_USERNAME") {
		t.Errorf("err = %v, want an invalid username rejected", err)
	}
}

func TestOwnerNoteExplainsHowOwnersAreKnown(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "pronium,TursunoffM"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.PersonaPrompt, "If someone asks how you know who your owners are") {
		t.Error("the persona does not say how to answer questions about who the owners are")
	}
}

func TestOwnersAreEquals(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "pronium,TursunoffM"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.PersonaPrompt, "Your owners are equals") {
		t.Error("the persona does not forbid ranking or mocking an owner")
	}
}

func TestImageGenerationSettings(t *testing.T) {
	cfg, err := Load(getenv(geminiEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ImagesEnabled || cfg.ImageModel != "gemini-3.1-flash-image" || cfg.DailyImageLimit != 2 {
		t.Errorf("images = %v, model = %q, limit = %d", cfg.ImagesEnabled, cfg.ImageModel, cfg.DailyImageLimit)
	}
	if cfg.ImageBaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("ImageBaseURL = %q, want Gemini's native API", cfg.ImageBaseURL)
	}
	if !strings.Contains(cfg.PersonaPrompt, "generate_image") {
		t.Error("the persona does not tell the model it can draw")
	}

	env := geminiEnv()
	env["ENABLE_IMAGE_GENERATION"] = "false"
	if cfg, _ := Load(getenv(env)); cfg.ImagesEnabled || strings.Contains(cfg.PersonaPrompt, "generate_image") {
		t.Error("image generation could not be turned off")
	}
	env = geminiEnv()
	env["DAILY_IMAGE_LIMIT"] = "0"
	if cfg, err := Load(getenv(env)); err != nil || cfg.DailyImageLimit != 0 {
		t.Errorf("DAILY_IMAGE_LIMIT=0 (owners only): %d, %v", cfg.DailyImageLimit, err)
	}
	if cfg, _ := Load(getenv(validEnv())); cfg.ImagesEnabled {
		t.Error("image generation is on without a Gemini key")
	}
}

func TestAllowedBots(t *testing.T) {
	cfg, err := Load(getenv(validEnv()))
	if err != nil || len(cfg.AllowedBots) != 0 || cfg.BotRepliesPer10Min != 5 {
		t.Fatalf("defaults: %q, %d, %v", cfg.AllowedBots, cfg.BotRepliesPer10Min, err)
	}
	env := validEnv()
	env["ALLOWED_BOTS"] = "@Other_Bot, helperbot"
	if cfg, err := Load(getenv(env)); err != nil || strings.Join(cfg.AllowedBots, ",") != "other_bot,helperbot" {
		t.Errorf("AllowedBots = %q, err = %v", cfg.AllowedBots, err)
	}
	env["ALLOWED_BOTS"] = "*"
	if cfg, err := Load(getenv(env)); err != nil || strings.Join(cfg.AllowedBots, ",") != "*" {
		t.Errorf("wildcard: %q, err = %v", cfg.AllowedBots, err)
	}
	env["ALLOWED_BOTS"] = "bad name"
	if _, err := Load(getenv(env)); err == nil || !strings.Contains(err.Error(), "ALLOWED_BOTS") {
		t.Errorf("err = %v", err)
	}
}

func TestOwnersAreCalledMaster(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "pronium"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.PersonaPrompt, `call them "master"`) {
		t.Error("the persona does not tell the model to call owners master")
	}
	if strings.Contains(cfg.PersonaPrompt, "Xo'jayin") {
		t.Error("an example still calls the owner xo'jayin; examples are copied")
	}
}

func TestPersonaCoversWhatWentWrongInTheGroup(t *testing.T) {
	env := validEnv()
	env["BOT_OWNER_USERNAME"] = "pronium"
	cfg, err := Load(getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	all := cfg.PersonaPrompt + cfg.GroupNote
	for _, want := range []string{
		"never restate or summarise their propaganda",
		"says you are rude",
		"Do not start a reply the way your recent replies started",
		"tells you to be quiet",
		"near-empty replies",
		"g'isht",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the prompt does not contain %q", want)
		}
	}
}
