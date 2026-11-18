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
	SetCommandMenus(context.Background(), setter, slog.New(slog.NewTextHandler(io.Discard, nil)))

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

// The menus keep the commands people use, the games behind /games, and none
// for the owners alone.
func TestCommandMenusAreShort(t *testing.T) {
	setter := &fakeCommandSetter{}
	SetCommandMenus(context.Background(), setter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for scope, commands := range setter.set {
		if len(commands) > 100 || !slices.ContainsFunc(commands, func(c telegram.BotCommand) bool { return c.Command == "games" }) ||
			slices.ContainsFunc(commands, func(c telegram.BotCommand) bool { return c.Command == "boss" || c.Command == "stats" }) {
			t.Errorf("%s menu %v", scope, commands)
		}
	}
}

// An admin, shown the admins' menu alone, finds everyone's commands in it
// too, each once.
func TestAdminsMenuHoldsTheMembers(t *testing.T) {
	admins := commandMenus[telegram.ScopeAllChatAdmins]
	seen := map[string]bool{}
	for _, c := range admins {
		if seen[c.Command] {
			t.Errorf("/%s twice in the admins' menu", c.Command)
		}
		seen[c.Command] = true
	}
	for _, c := range commandMenus[telegram.ScopeAllGroupChats] {
		if !seen[c.Command] {
			t.Errorf("/%s is missing from the admins' menu", c.Command)
		}
	}
}

// A failure is logged and does not stop the start.
func TestSetCommandMenusFailure(t *testing.T) {
	setter := &fakeCommandSetter{err: errors.New("telegram is down")}
	SetCommandMenus(context.Background(), setter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(setter.set) != 3 {
		t.Errorf("tried %d menus, want all three despite the failures", len(setter.set))
	}
}

type fakeRightsSetter struct {
	set map[bool]telegram.AdministratorRights
	err error
}

func (f *fakeRightsSetter) SetMyDefaultAdministratorRights(_ context.Context, rights telegram.AdministratorRights, forChannels bool) error {
	if f.set == nil {
		f.set = map[bool]telegram.AdministratorRights{}
	}
	f.set[forChannels] = rights
	return f.err
}

// Every right a group action needs is asked for, as the moderation and
// topics tools name them, and nothing to hide or post as the group; a
// channel gets posting and editing. A failure stops neither.
func TestSetAdminRights(t *testing.T) {
	setter := &fakeRightsSetter{err: errors.New("telegram is down")}
	SetAdminRights(context.Background(), setter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	group, channel := setter.set[false], setter.set[true]
	asked := telegram.ChatMember{Status: "administrator", CanDeleteMessages: group.CanDeleteMessages, CanRestrictMembers: group.CanRestrictMembers,
		CanPinMessages: group.CanPinMessages, CanPromoteMembers: group.CanPromoteMembers, CanChangeInfo: group.CanChangeInfo,
		CanInviteUsers: group.CanInviteUsers, CanManageTopics: group.CanManageTopics}
	for _, right := range []string{telegram.RightDeleteMessages, telegram.RightRestrictMembers, telegram.RightPinMessages, telegram.RightPromoteMembers,
		telegram.RightChangeInfo, telegram.RightInviteUsers, telegram.RightManageTopics} {
		if !asked.Has(right) {
			t.Errorf("groups: %s is not asked for", right)
		}
	}
	if group.IsAnonymous || group.CanPostMessages || group.CanManageVideoChats {
		t.Errorf("groups: %+v asks for more than the bot uses", group)
	}
	if len(setter.set) != 2 || !channel.CanPostMessages || !channel.CanEditMessages || channel.CanDeleteMessages || channel.CanPromoteMembers {
		t.Errorf("channels: %+v", channel)
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

type fakeChatCommandSetter struct {
	set map[int64][]telegram.BotCommand
}

func (f *fakeChatCommandSetter) SetChatCommands(_ context.Context, chatID int64, commands []telegram.BotCommand) error {
	f.set[chatID] = commands
	return nil
}

// Each owner gets every command in their private chat's menu.
func TestOwnerPrivateMenus(t *testing.T) {
	setter := &fakeChatCommandSetter{set: map[int64][]telegram.BotCommand{}}
	SetOwnerMenus(context.Background(), setter, map[int64]bool{7: true, 8: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, owner := range []int64{7, 8} {
		if !slices.Equal(setter.set[owner], ownerMenu) {
			t.Errorf("owner %d menu = %v, want every command", owner, setter.set[owner])
		}
	}
}

// The owners' menu holds every command of the others' menus and the
// owners' own, each once, as many as Telegram takes, each one it accepts.
func TestOwnerMenuHoldsEveryCommand(t *testing.T) {
	if len(ownerMenu) > 100 {
		t.Errorf("owners' menu has %d commands, Telegram takes 100", len(ownerMenu))
	}
	valid := regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	seen := map[string]bool{}
	for _, c := range ownerMenu {
		if seen[c.Command] {
			t.Errorf("/%s twice in the owners' menu", c.Command)
		}
		seen[c.Command] = true
		if !valid.MatchString(c.Command) || c.Description == "" || len([]rune(c.Description)) > 256 {
			t.Errorf("%+v is not a command Telegram takes", c)
		}
	}
	for scope, commands := range commandMenus {
		for _, c := range commands {
			if !seen[c.Command] {
				t.Errorf("/%s of the %s menu is missing from the owners' menu", c.Command, scope)
			}
		}
	}
	for _, command := range []string{"limit", "active", "aichat", "aigroups", "announce", "payments", "refund",
		"model", "stats", "settings", "set", "business", "import_stickers", "premium", "paysupport",
		"game", "snake", "woodcutter", "boss", "mafia", "checkers", "card", "mycards", "cardtop", "market",
		"duel", "teams", "invites", "invites_month", "invites_auto", "unpin", "unignore", "ping", "about"} {
		if !seen[command] {
			t.Errorf("/%s is missing from the owners' menu", command)
		}
	}
}
