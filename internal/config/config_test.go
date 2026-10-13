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
		"db_POSTGRES_URL":    testDBURL,
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
	if cfg.OpenRouterBaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("OpenRouterBaseURL = %q", cfg.OpenRouterBaseURL)
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
		"RetentionDays":          {cfg.RetentionDays, 90},
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
	for _, name := range []string{"TELEGRAM_BOT_TOKEN", "OPENROUTER_API_KEY", "db_POSTGRES_URL"} {
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
	if cfg.BotDisplayName != "Mira" || len(cfg.WakeWords) != 1 || cfg.WakeWords[0] != "mira" {
		t.Errorf("name = %q, wake words = %q", cfg.BotDisplayName, cfg.WakeWords)
	}
	if cfg.AmbientMessages != 20 || cfg.MaxGroupReplyChars != 4000 || cfg.GroupRateLimitPerMinute != 10 {
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
	for _, want := range []string{"slur", "untrusted", "threat"} {
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
	if cfg.PersonaPrompt != "You are Mira, a grumpy cat. Mira hates Mondays." {
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
	for value, ok := range map[string]bool{"0": true, "100": true, "-1": false, "101": false} {
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
