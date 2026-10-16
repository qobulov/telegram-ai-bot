package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// Every tool has a default (UTC, a help that offers everything), so nothing
// else would notice if the configuration stopped reaching them.
func TestBuiltinToolsFollowTheConfiguration(t *testing.T) {
	tashkent, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		cfg         config.Config
		draw, voice bool
	}{
		"listens":               {config.Config{Location: tashkent, AudioInput: true}, false, true},
		"draws":                 {config.Config{Location: tashkent, ImagesEnabled: true}, true, false},
		"cannot draw or listen": {config.Config{Location: tashkent}, false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			builtin := builtinTools(tc.cfg, nil, Bot{User: telegram.User{ID: 777}}, tools.NewModeration(tools.ModerationOptions{}).Tool())

			var now struct {
				Timezone string `json:"timezone"`
			}
			runTool(t, builtin, "get_current_time", &now)
			if now.Timezone != "Asia/Tashkent" {
				t.Errorf("get_current_time without a timezone answered in %q, want Asia/Tashkent", now.Timezone)
			}
			var help struct {
				Help string `json:"help"`
			}
			runTool(t, builtin, "get_bot_help", &help)
			if got := strings.Contains(help.Help, "draw a picture"); got != tc.draw {
				t.Errorf("help offers drawing = %t, want %t", got, tc.draw)
			}
			if got := strings.Contains(help.Help, "voice messages"); got != tc.voice {
				t.Errorf("help offers listening = %t, want %t", got, tc.voice)
			}
			if got := hasTool(builtin, "generate_image"); got != tc.draw {
				t.Errorf("generate_image registered = %t, want %t", got, tc.draw)
			}
		})
	}
}

// runTool runs the named tool with no arguments and decodes its result.
func runTool(t *testing.T, builtin []tools.Tool, name string, out any) {
	t.Helper()
	for _, tool := range builtin {
		if tool.Name != name {
			continue
		}
		result, err := tool.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no %s tool", name)
}

// The live-data tools sit beside the built-in ones in one registry, and
// football, in the tools and in the help, needs its token.
func TestLiveDataToolsFollowTheConfiguration(t *testing.T) {
	for _, token := range []string{"", "fd-token"} {
		cfg := config.Config{Location: time.UTC, FootballDataToken: token}
		bot := Bot{User: telegram.User{ID: 777, Username: "maria_bot"}}
		live := liveDataTools(cfg, bot)
		for _, name := range []string{"currency_rate", "get_weather", "prayer_times", "news_headlines", "wiki_lookup", "crypto_price"} {
			if !hasTool(live, name) {
				t.Errorf("token %q: no %s tool", token, name)
			}
		}
		if got := hasTool(live, "football"); got != (token != "") {
			t.Errorf("token %q: football registered = %t", token, got)
		}
		builtin := builtinTools(cfg, nil, bot, tools.NewModeration(tools.ModerationOptions{}).Tool())
		if _, err := tools.NewRegistry(cfg.IsAdmin, nil, append(builtin, live...)...); err != nil {
			t.Errorf("token %q: NewRegistry: %v", token, err)
		}
		var help struct {
			Help string `json:"help"`
		}
		runTool(t, builtin, "get_bot_help", &help)
		if got := strings.Contains(help.Help, "football"); got != (token != "") {
			t.Errorf("token %q: help offers football = %t", token, got)
		}
	}
}

func hasTool(builtin []tools.Tool, name string) bool {
	for _, tool := range builtin {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// group_admins is there for every group, with or without owners, and knows
// the bot by its ID and the owners by theirs.
func TestGroupAdminsKnowsTheBotAndItsOwners(t *testing.T) {
	telegramAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getChatAdministrators") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":[
			{"status":"creator","user":{"id":9001,"first_name":"Javohir","username":"pronium"}},
			{"status":"administrator","user":{"id":777,"is_bot":true,"first_name":"Maria","username":"maria_bot"}}]}`)
	}))
	t.Cleanup(telegramAPI.Close)
	bot := Bot{
		Client: telegram.NewClient(telegram.Options{Token: "test-token", BaseURL: telegramAPI.URL}),
		User:   telegram.User{ID: 777, Username: "maria_bot"},
	}
	cases := map[string]struct {
		cfg   config.Config
		owner string
	}{
		"owners by ID": {config.Config{Location: time.UTC, OwnerUsernames: []string{"pronium"}, OwnerIDs: map[int64]bool{9001: true}}, `,"owner":true`},
		"no owners":    {config.Config{Location: time.UTC}, ""},
		// A username can be given up and taken by someone else, so only an
		// ID marks an owner.
		"owners by username only": {config.Config{Location: time.UTC, OwnerUsernames: []string{"pronium"}}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			builtin := builtinTools(tc.cfg, nil, bot, tools.NewModeration(tools.ModerationOptions{}).Tool())
			registry, err := tools.NewRegistry(tc.cfg.IsAdmin, nil, builtin...)
			if err != nil {
				t.Fatal(err)
			}
			member := tools.WithTurn(context.Background(), tools.Caller{UserID: 5005, ChatID: -100123, Human: true, UpdateID: 1}, &tools.Sink{})

			got := registry.Execute(member, 5005, "group_admins", "{}")

			want := `{"name":"Javohir","username":"@pronium","role":"creator"` + tc.owner + `},` +
				`{"name":"Maria","username":"@maria_bot","role":"administrator","you":true,"bot":true}]`
			if !strings.Contains(got, `"admins":[`+want) {
				t.Errorf("group_admins = %s\nwant the admins %s", got, want)
			}
		})
	}
}
