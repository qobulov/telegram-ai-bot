package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

type fakeCommandSetter struct {
	set map[string][]telegram.BotCommand
	err error
}

func (f *fakeCommandSetter) SetMyCommands(_ context.Context, scope string, commands []telegram.BotCommand) error {
	if f.set == nil {
		f.set = map[string][]telegram.BotCommand{}
	}
	f.set[scope] = commands
	return f.err
}

// Every menu is set, admins get the commands only they may use, and every
// command is one Telegram accepts.
func TestSetCommandMenus(t *testing.T) {
	setter := &fakeCommandSetter{}
	SetCommandMenus(context.Background(), setter, false, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if len(setter.set) != 3 {
		t.Fatalf("menus = %v, want private chats, groups and admins", setter.set)
	}
	names := func(scope string) []string {
		var out []string
		for _, c := range setter.set[scope] {
			out = append(out, c.Command)
		}
		return out
	}
	if !slices.Contains(names(telegram.ScopeAllChatAdmins), "mute") || slices.Contains(names(telegram.ScopeAllGroupChats), "mute") {
		t.Errorf("admins %v, groups %v: only admins may mute the bot", names(telegram.ScopeAllChatAdmins), names(telegram.ScopeAllGroupChats))
	}
	valid := regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	for scope, commands := range setter.set {
		for _, c := range commands {
			if !valid.MatchString(c.Command) || c.Description == "" || len([]rune(c.Description)) > 256 {
				t.Errorf("%s: %+v is not a command Telegram takes", scope, c)
			}
		}
	}
}

// With the pages served, every menu offers /stats and /game; without, none.
func TestSetCommandMenusWithPages(t *testing.T) {
	for _, pages := range []bool{false, true} {
		setter := &fakeCommandSetter{}
		SetCommandMenus(context.Background(), setter, pages, slog.New(slog.NewTextHandler(io.Discard, nil)))
		for scope, commands := range setter.set {
			has := slices.ContainsFunc(commands, func(c telegram.BotCommand) bool { return c.Command == "stats" }) &&
				slices.ContainsFunc(commands, func(c telegram.BotCommand) bool { return c.Command == "game" })
			if has != pages {
				t.Errorf("pages %t: %s menu %v", pages, scope, commands)
			}
		}
	}
	if slices.ContainsFunc(commandMenus[telegram.ScopeDefault], func(c telegram.BotCommand) bool { return c.Command == "stats" }) {
		t.Error("the page commands were added to the menus themselves")
	}
}

// A failure is logged and does not stop the start.
func TestSetCommandMenusFailure(t *testing.T) {
	setter := &fakeCommandSetter{err: errors.New("telegram is down")}
	SetCommandMenus(context.Background(), setter, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(setter.set) != 3 {
		t.Errorf("tried %d menus, want all three despite the failures", len(setter.set))
	}
}

// The swear judge thinks little whatever answers do: at medium its
// thinking filled its few tokens and cut its one word off.
func TestSwearJudgeThinksLittle(t *testing.T) {
	for effort, want := range map[string]string{"": "", "none": "none", "minimal": "minimal", "low": "low", "medium": "low", "high": "low", "default": "low"} {
		if got := judgeReasoning(effort); got != want {
			t.Errorf("judgeReasoning(%q) = %q, want %q", effort, got, want)
		}
	}
}
