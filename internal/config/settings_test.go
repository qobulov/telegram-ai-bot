package config

import (
	"strings"
	"testing"
)

// Every setting /set may change is one Load reads: a value it cannot read
// is refused under that name.
func TestEverySettingIsReadByLoad(t *testing.T) {
	for _, setting := range Settings {
		err := CheckSetting(getenv(validEnv()), setting, "not-a-value")
		if err == nil || !strings.Contains(err.Error(), setting.Name) {
			t.Errorf("%s: %v, want it refused by name", setting.Name, err)
		}
	}
}

func TestCheckSettingBoundsAndAccepts(t *testing.T) {
	limit, _ := FindSetting("max_output_tokens")
	if err := CheckSetting(getenv(validEnv()), limit, "100000"); err == nil || !strings.Contains(err.Error(), "at most 8192") {
		t.Errorf("over the bound: %v", err)
	}
	if err := CheckSetting(getenv(validEnv()), limit, "2048"); err != nil {
		t.Errorf("2048: %v", err)
	}
	mode, _ := FindSetting("PRIVATE_CHATS")
	if err := CheckSetting(getenv(validEnv()), mode, "owners"); err != nil {
		t.Errorf("owners: %v", err)
	}
	if _, ok := FindSetting("TELEGRAM_BOT_TOKEN"); ok {
		t.Error("a secret can be set")
	}
}

func TestOverlayPutsSavedOverTheEnvironment(t *testing.T) {
	env := validEnv()
	env["MAX_OUTPUT_TOKENS"] = "1000"
	cfg, err := Load(Overlay(getenv(env), map[string]string{"MAX_OUTPUT_TOKENS": "2048", "PRIVATE_CHATS": "off"}))
	if err != nil || cfg.MaxOutputTokens != 2048 || cfg.PrivateChats != "off" {
		t.Errorf("cfg = %d %q, %v", cfg.MaxOutputTokens, cfg.PrivateChats, err)
	}
}
