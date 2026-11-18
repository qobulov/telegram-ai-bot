package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/telegram"
)

// To anyone but an owner the commands do not exist, and nothing changes.
func TestSettingsAreForOwnersOnly(t *testing.T) {
	f := newFixture()
	rules := &fakeSettings{}
	f.opts.Settings = rules

	sent := f.run(textFrom(userID, "/set MAX_OUTPUT_TOKENS 50"))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "bilmayman") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v; want the unknown-command reply and nothing saved", sent, rules.all())
	}
}

// The owners' own commands work only in private: in a group, an owner's
// too, they get no answer, change nothing and do not reach the model.
func TestOwnerCommandsAreSilentInAGroup(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	rules := &fakeSettings{}
	f.opts.Settings = rules
	var sent []sentMessage
	for i, command := range slices.Sorted(maps.Keys(privateOwnerCommands)) {
		msg := fromOwner(groupMessage(command + "@MyBot 10"))
		msg.MessageID = int64(40 + i)
		sent = append(sent, f.run(inGroup(msg))...)
	}

	if len(sent) != 0 || len(rules.all()) != 0 || len(f.replier.recorded()) != 0 {
		t.Errorf("sent %+v, settings %v, model calls %d; want nothing", sent, rules.all(), len(f.replier.recorded()))
	}
}

type fakeMenuSetter struct{ calls [][2]int64 }

func (f *fakeMenuSetter) SetMemberCommands(_ context.Context, chatID, userID int64, _ []telegram.BotCommand) error {
	f.calls = append(f.calls, [2]int64{chatID, userID})
	return nil
}

// An owner who writes in a group is given the owners' menu there, once; a
// member is not.
func TestOwnerGetsTheOwnersMenuOnce(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	menus := &fakeMenuSetter{}
	f.opts.MenuSetter, f.opts.OwnerMenu = menus, []telegram.BotCommand{{Command: "limit", Description: "x"}}
	second := fromOwner(groupMessage("yana"))
	second.MessageID = 31

	f.run(inGroup(fromOwner(groupMessage("salom"))), inGroup(second), inGroup(groupMessage("men a'zoman")))

	if len(menus.calls) != 1 || menus.calls[0] != [2]int64{groupID, 9001} {
		t.Errorf("menus set = %v, want once, for the owner in this group", menus.calls)
	}
}

// fakeSettings keeps settings in memory, by chat.
type fakeSettings struct {
	mu   sync.Mutex
	byID map[int64]map[string]string
}

func (f *fakeSettings) Settings(_ context.Context, chatID int64) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.byID[chatID]), nil
}

func (f *fakeSettings) SetSetting(_ context.Context, chatID int64, name, value string, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID == nil {
		f.byID = map[int64]map[string]string{}
	}
	if f.byID[chatID] == nil {
		f.byID[chatID] = map[string]string{}
	}
	f.byID[chatID][name] = value
	return nil
}

func (f *fakeSettings) ClearSetting(_ context.Context, chatID int64, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID[chatID], name)
	return nil
}

// all are every chat's settings, as "chat NAME=value".
func (f *fakeSettings) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for chat, settings := range f.byID {
		for name, value := range settings {
			out = append(out, fmt.Sprintf("%d %s=%s", chat, name, value))
		}
	}
	slices.Sort(out)
	return out
}

// /set in a private chat changes the bot's setting, checked first.
func TestOwnerSetsSettings(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	settings := &fakeSettings{}
	f.opts.Settings = settings
	f.opts.CheckSetting = func(setting config.Setting, value string) error {
		if value == "lots" {
			return fmt.Errorf("%s must be a whole number", setting.Name)
		}
		return nil
	}
	private := fromOwner(textFrom(9001, "/set max_output_tokens 2048").Message)
	bad := fromOwner(textFrom(9001, "/set MAX_OUTPUT_TOKENS lots").Message)
	bad.MessageID = 2

	sent := f.run(telegram.Update{UpdateID: 1, Message: private}, telegram.Update{UpdateID: 2, Message: bad})

	want := []string{"0 MAX_OUTPUT_TOKENS=2048"}
	if got := settings.all(); !slices.Equal(got, want) {
		t.Errorf("settings = %q, want %q", got, want)
	}
	// The workers answer in any order.
	var texts []string
	for _, m := range sent {
		texts = append(texts, m.text)
	}
	all := strings.Join(texts, "\n")
	if len(sent) != 2 || !strings.Contains(all, "MAX_OUTPUT_TOKENS = 2048; bir daqiqa ichida") || !strings.Contains(all, "whole number") {
		t.Errorf("sent %+v", sent)
	}
}
