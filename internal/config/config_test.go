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
		"DATABASE_URL":       testDBURL,
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
	if cfg.SystemPrompt == "" {
		t.Error("SystemPrompt is empty")
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
	for _, name := range []string{"TELEGRAM_BOT_TOKEN", "OPENROUTER_API_KEY", "DATABASE_URL"} {
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
