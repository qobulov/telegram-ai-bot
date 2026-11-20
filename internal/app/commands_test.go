package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
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
	for _, command := range []string{"aigroups", "announce", "payments", "refund",
		"model", "stats", "settings", "set", "business", "import_stickers", "premium", "paysupport",
		"woodcutter", "boss", "mafia", "card", "teams", "invites", "invites_auto", "unpin", "unignore", "ping", "about"} {
		if !seen[command] {
			t.Errorf("/%s is missing from the owners' menu", command)
		}
	}
	for _, gone := range []string{"game", "snake", "tictactoe", "checkers"} {
		if seen[gone] {
			t.Errorf("/%s, a removed game's, is in the owners' menu", gone)
		}
	}
}

// In a group an owner's menu is the owners' menu without their own
// commands, which work only in private.
func TestOwnerGroupMenuLeavesOutTheOwnersOwn(t *testing.T) {
	if want := len(ownerGroupMenu) + len(ownerOnlyMenu); len(ownerMenu) != want {
		t.Errorf("owners' menu has %d commands, want %d", len(ownerMenu), want)
	}
	for _, c := range ownerGroupMenu {
		if slices.ContainsFunc(ownerOnlyMenu, func(o telegram.BotCommand) bool { return o.Command == c.Command }) {
			t.Errorf("/%s, an owner's own, is in the group menu", c.Command)
		}
	}
	for _, c := range ownerOnlyMenu {
		if !strings.HasSuffix(c.Description, "(owner)") {
			t.Errorf("/%s is not marked (owner)", c.Command)
		}
	}
}

func menuNames(menu []telegram.BotCommand) []string {
	var names []string
	for _, c := range menu {
		names = append(names, c.Command)
	}
	return names
}

// A moderator's menu has what their rank opens, then everyone's commands,
// each once and each one Telegram takes.
func TestRankMenuFollowsTheRank(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	for rank := 1; rank <= 5; rank++ {
		menu := RankMenu(rank, nil)
		seen := map[string]bool{}
		for _, c := range menu {
			if seen[c.Command] || !valid.MatchString(c.Command) || c.Description == "" || len([]rune(c.Description)) > 256 {
				t.Errorf("rank %d: %+v is twice or not a command Telegram takes", rank, c)
			}
			seen[c.Command] = true
		}
		if len(menu) > 100 {
			t.Errorf("rank %d: %d commands, Telegram takes 100", rank, len(menu))
		}
		for _, c := range membersMenu {
			if !seen[c.Command] {
				t.Errorf("rank %d: /%s, everyone's, is missing", rank, c.Command)
			}
		}
	}
	first := menuNames(RankMenu(1, nil))
	for _, want := range []string{"ban", "unban", "kick", "mute", "unmute", "warn", "unwarn", "warns", "banlist", "reports"} {
		if !slices.Contains(first, want) {
			t.Errorf("rank 1 menu %v lacks /%s", first, want)
		}
	}
	for _, not := range []string{"ranklog", "pin", "del", "rank", "kickinactive", "promote", "amnesty", "access", "ignore", "invites_auto"} {
		if slices.Contains(first, not) {
			t.Errorf("rank 1 menu %v has /%s, which rank 1 does not open", first, not)
		}
	}
	top := menuNames(RankMenu(5, nil))
	for _, c := range rankedCommands {
		if !slices.Contains(top, c.name) {
			t.Errorf("rank 5 menu %v lacks /%s", top, c.name)
		}
	}
	for _, not := range []string{"ignore", "unignore", "invites_auto"} {
		if slices.Contains(top, not) {
			t.Errorf("rank 5 menu has /%s, which takes a Telegram admin", not)
		}
	}
}

// A group's own ranks for the commands move them in or out of the menu.
func TestRankMenuKeepsTheGroupsAccess(t *testing.T) {
	menu := menuNames(RankMenu(2, map[string]int{"ban": 3, "delete": 2}))
	if slices.Contains(menu, "ban") || slices.Contains(menu, "unban") || !slices.Contains(menu, "del") || !slices.Contains(menu, "pin") {
		t.Errorf("rank 2 with ban at 3 and delete at 2: %v", menu)
	}
	if menu := menuNames(RankMenu(0, nil)); slices.Contains(menu, "ban") || len(menu) != len(membersMenu) {
		t.Errorf("no rank: %v, want everyone's alone", menu)
	}
}

// Every command ranks open in the menu is one the admins' menu or the
// owners' has, so its description is there to reuse.
func TestRankedCommandsAreKnown(t *testing.T) {
	known := menuNames(ownerGroupMenu)
	for _, c := range rankedCommands {
		if !slices.Contains(known, c.name) {
			t.Errorf("/%s has no description", c.name)
		}
	}
}

func commandNames(menu []telegram.BotCommand) []string {
	names := make([]string, len(menu))
	for i, c := range menu {
		names[i] = c.Command
	}
	return names
}

// A group's admins see the moderation commands the bot's rights there let
// it carry out, then everyone's: none where it is no admin, the restricting
// ones alone where it may only restrict, all of them with every right.
func TestGroupAdminMenuFollowsTheBotsRights(t *testing.T) {
	members := commandNames(membersMenu)
	if got := commandNames(GroupAdminMenu(nil)); !slices.Equal(got, members) {
		t.Errorf("no rights: %v, want the members' menu alone", got)
	}

	restrictOnly := commandNames(GroupAdminMenu([]string{telegram.RightRestrictMembers}))
	for _, want := range []string{"warn", "unwarn", "mute", "unmute", "ban", "unban", "kick", "kickinactive", "amnesty", "banlist", "warns", "reports", "antispam", "rank", "access", "ignore"} {
		if !slices.Contains(restrictOnly, want) {
			t.Errorf("restrict only: /%s missing from %v", want, restrictOnly)
		}
	}
	for _, not := range []string{"del", "pin", "unpin", "promote", "demote", "tag", "cleanservice"} {
		if slices.Contains(restrictOnly, not) {
			t.Errorf("restrict only: /%s in %v", not, restrictOnly)
		}
	}
	if !slices.Equal(restrictOnly[len(restrictOnly)-len(members):], members) {
		t.Errorf("restrict only: %v does not end with the members' menu", restrictOnly)
	}

	deleteOnly := commandNames(GroupAdminMenu([]string{telegram.RightDeleteMessages}))
	for _, want := range []string{"del", "cleanservice", "reports", "antispam"} {
		if !slices.Contains(deleteOnly, want) {
			t.Errorf("delete only: /%s missing from %v", want, deleteOnly)
		}
	}
	if slices.Contains(deleteOnly, "ban") {
		t.Errorf("delete only: /ban in %v", deleteOnly)
	}
	if tags := commandNames(GroupAdminMenu([]string{telegram.RightManageTags})); !slices.Contains(tags, "tag") || slices.Contains(tags, "promote") {
		t.Errorf("tags only: %v, want /tag and no /promote", tags)
	}

	full := GroupAdminMenu([]string{telegram.RightDeleteMessages, telegram.RightRestrictMembers, telegram.RightPinMessages,
		telegram.RightPromoteMembers, telegram.RightManageTags, telegram.RightChangeInfo, telegram.RightInviteUsers})
	if !slices.Equal(full, commandMenus[telegram.ScopeAllChatAdmins]) {
		t.Errorf("every right: %v, want the menu of every group's admins %v", commandNames(full), commandNames(commandMenus[telegram.ScopeAllChatAdmins]))
	}
}

// Admins may run /unignore and /invites_auto, so their menu lists them; a
// ranked moderator's /mute promises nothing about muting the bot, which
// takes a Telegram admin.
func TestMenusListWhatTheirRoleMayRun(t *testing.T) {
	admins := commandNames(commandMenus[telegram.ScopeAllChatAdmins])
	for _, want := range []string{"ignore", "unignore", "invites_auto"} {
		if !slices.Contains(admins, want) {
			t.Errorf("the admins' menu lacks /%s", want)
		}
	}
	for _, c := range RankMenu(5, nil) {
		if (c.Command == "mute" || c.Command == "unmute") && strings.Contains(c.Description, "bot") {
			t.Errorf("a moderator's /%s = %q, which they cannot do to the bot", c.Command, c.Description)
		}
	}
}
