package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/telegram"
)

// An owner's /limit sets the group's answers a minute, which the quota then
// uses; /limit default goes back to the configured one.
func TestOwnerSetsTheGroupReplyLimit(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	rules, quota := &fakeSettings{}, &fakeGroupQuota{}
	f.opts.Settings, f.opts.GroupReplies, f.opts.GroupRatePerMinute = rules, quota, 6

	sent := f.run(inGroup(fromOwner(groupMessage("/limit@MyBot 10"))))
	expectOneReply(t, sent, "Endi bu guruhda bot daqiqasiga 10 ta javob beradi.")

	question := groupMessage("Mira, salom")
	question.MessageID = 31
	f.run(inGroup(question))
	if calls := quota.calls; len(calls) != 1 || calls[0].PerMinute != 10 {
		t.Errorf("quota calls = %+v, want one at 10 a minute", calls)
	}

	reset := fromOwner(groupMessage("/limit@MyBot default"))
	reset.MessageID = 32
	sent = f.run(inGroup(reset))
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "umumiy limit qaytdi: daqiqasiga 6 ta") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v; want the default back", sent, rules.all())
	}
}

// /active off lets a quiet group be answered; /active 3 sets the bar there.
func TestOwnerTurnsTheActiveMembersRule(t *testing.T) {
	for name, c := range map[string]struct {
		command  string
		answered bool
	}{
		"off":         {"/active@MyBot off", true},
		"three":       {"/active@MyBot 3", true},
		"ten":         {"/active@MyBot 10", false},
		"not changed": {"/active@MyBot", false},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.OwnerUsernames = []string{ownerUsername}
			f.opts.Settings = &fakeSettings{}
			f.opts.MinActiveMembers, f.opts.ActiveWindow, f.opts.ActiveCounter = 5, 72*time.Hour, &fakeActives{count: 4}

			f.run(inGroup(fromOwner(groupMessage(c.command))))
			question := groupMessage("Mira, salom")
			question.MessageID = 31
			f.run(inGroup(question))

			if calls := len(f.replier.recorded()); (calls == 1) != c.answered {
				t.Errorf("model calls = %d, want answered %v", calls, c.answered)
			}
		})
	}
}

// To anyone but an owner the commands do not exist, and nothing changes.
func TestSettingsAreForOwnersOnly(t *testing.T) {
	f := newFixture()
	rules := &fakeSettings{}
	f.opts.Settings = rules

	sent := f.run(inGroup(asAdmin(groupMessage("/limit@MyBot 50"))))

	if len(sent) != 1 || strings.Contains(sent[0].text, "50") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v; want the unknown-command reply and nothing saved", sent, rules.all())
	}
}

// A number out of range is refused with how to write it.
func TestSettingsRefuseABadNumber(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	rules := &fakeSettings{}
	f.opts.Settings = rules

	sent := f.run(inGroup(fromOwner(groupMessage("/limit@MyBot 500"))))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "1 dan 60 gacha") || len(rules.all()) != 0 {
		t.Errorf("sent %+v, rules %+v", sent, rules.all())
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

// /set in a private chat changes the bot's setting, checked first; in a
// group a group setting changes the group's, and a bot-wide one is refused.
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
	inGroupSet := fromOwner(groupMessage("/set@MyBot GROUP_ACTIVE_DAYS 7"))
	globalInGroup := fromOwner(groupMessage("/set@MyBot RETENTION_DAYS 3"))
	globalInGroup.MessageID = 31

	sent := f.run(telegram.Update{UpdateID: 1, Message: private}, telegram.Update{UpdateID: 2, Message: bad}, inGroup(inGroupSet), inGroup(globalInGroup))

	want := []string{"-1001234567890 GROUP_ACTIVE_DAYS=7", "0 MAX_OUTPUT_TOKENS=2048"}
	if got := settings.all(); !slices.Equal(got, want) {
		t.Errorf("settings = %q, want %q", got, want)
	}
	// The workers answer in any order.
	var texts []string
	for _, m := range sent {
		texts = append(texts, m.text)
	}
	all := strings.Join(texts, "\n")
	if len(sent) != 4 || !strings.Contains(all, "MAX_OUTPUT_TOKENS = 2048; bir daqiqa ichida") || !strings.Contains(all, "whole number") ||
		!strings.Contains(all, "RETENTION_DAYS faqat umumiy") {
		t.Errorf("sent %+v", sent)
	}
}

// /settings lists the group's settings over the bot's; to a member it is
// not a command.
func TestSettingsListsAGroupsOwnOverTheBots(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	f.opts.Settings = &fakeSettings{byID: map[int64]map[string]string{
		0: {"GROUP_RATE_LIMIT_PER_MINUTE": "8"}, groupID: {"GROUP_ACTIVE_DAYS": "7"},
	}}
	f.opts.Getenv = func(string) string { return "" }

	sent := f.run(inGroup(fromOwner(groupMessage("/settings@MyBot"))))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "GROUP_RATE_LIMIT_PER_MINUTE = 8 (umumiy)") ||
		!strings.Contains(sent[0].text, "GROUP_ACTIVE_DAYS = 7 (guruh)") || strings.Contains(sent[0].text, "RETENTION_DAYS") {
		t.Errorf("sent %+v", sent)
	}
}
