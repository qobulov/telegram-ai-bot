package service

import (
	"context"
	"slices"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// fakeAdminMenus records the admins' menus set, by group, as the rights
// each was made from.
type fakeAdminMenus struct {
	mu   sync.Mutex
	sets []adminMenuSet
}

type adminMenuSet struct {
	chatID int64
	menu   []string
}

func (f *fakeAdminMenus) SetChatAdminCommands(_ context.Context, chatID int64, commands []telegram.BotCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.Command
	}
	f.sets = append(f.sets, adminMenuSet{chatID, names})
	return nil
}

func (f *fakeAdminMenus) recorded() []adminMenuSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sets)
}

// testAdminMenu stands in for the app's: each right held names itself, and
// "help" stands for everyone's commands.
func testAdminMenu(rights []string) []telegram.BotCommand {
	menu := make([]telegram.BotCommand, 0, len(rights)+1)
	for _, r := range rights {
		menu = append(menu, telegram.BotCommand{Command: r, Description: "x"})
	}
	return append(menu, telegram.BotCommand{Command: "help", Description: "x"})
}

func withAdminMenus(f *fixture) *fakeAdminMenus {
	menus := &fakeAdminMenus{}
	f.opts.AdminMenu, f.opts.AdminMenuSetter = testAdminMenu, menus
	return menus
}

// The bot made an admin sets its admins' menu there from its new rights;
// taken back to a member, the menu is everyone's alone; a private chat or
// the bot leaving sets none.
func TestTheBotsMembershipSetsTheAdminsMenu(t *testing.T) {
	f := newFixture()
	menus := withAdminMenus(f)
	group := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup, Title: "Devs"}
	promoted := telegram.ChatMember{Status: "administrator", CanRestrictMembers: true}

	f.run(memberChange(group, promoted), membershipChange(group, "member"), membershipChange(group, "left"),
		membershipChange(telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}, "member"))

	want := []adminMenuSet{
		{groupID, []string{telegram.RightRestrictMembers, "help"}},
		{groupID, []string{"help"}},
	}
	if got := menus.recorded(); !slices.EqualFunc(got, want, func(a, b adminMenuSet) bool { return a.chatID == b.chatID && slices.Equal(a.menu, b.menu) }) {
		t.Errorf("menus set = %+v, want %+v", got, want)
	}
}

// A group joined before is given its admins' menu from the stored rights
// when someone writes there, once a day, not on every message.
func TestAGroupsAdminsMenuIsSetOnceWhenSomeoneWrites(t *testing.T) {
	f := newFixture()
	menus := withAdminMenus(f)
	rights := &fakeBotRights{rights: []string{telegram.RightPinMessages}}
	f.opts.BotRights = rights
	second := groupMessage("yana")
	second.MessageID = 31

	f.run(inGroup(groupMessage("salom")), inGroup(second))

	got := menus.recorded()
	if len(got) != 1 || got[0].chatID != groupID || !slices.Equal(got[0].menu, []string{telegram.RightPinMessages, "help"}) {
		t.Errorf("menus set = %+v, want once, from the stored rights", got)
	}
}

// A group whose menu the bot's membership just set is not set again when
// someone writes there.
func TestAMembershipChangeSparesTheDailyAdminsMenu(t *testing.T) {
	f := newFixture()
	menus := withAdminMenus(f)
	f.opts.BotRights = &fakeBotRights{}
	group := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup, Title: "Devs"}

	f.run(memberChange(group, telegram.ChatMember{Status: "administrator", CanDeleteMessages: true}), inGroup(groupMessage("salom")))

	if got := menus.recorded(); len(got) != 1 || !slices.Equal(got[0].menu, []string{telegram.RightDeleteMessages, "help"}) {
		t.Errorf("menus set = %+v, want the promotion's alone", got)
	}
}
