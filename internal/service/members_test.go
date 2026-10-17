package service

import (
	"context"
	"slices"
	"sync"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeRoster records the members the service notes.
type fakeRoster struct {
	mu   sync.Mutex
	seen []storage.GroupMember
}

func (f *fakeRoster) SeeMember(_ context.Context, m storage.GroupMember) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, m)
	return nil
}

func (f *fakeRoster) all() []storage.GroupMember {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// The Bot API cannot list a group's members, so the service notes everyone
// a group shows it: senders, who joined or left, and Telegram's
// chat_member reports. The bot itself and private chats are not noted.
func TestMembersAreNotedFromWhatTheGroupShows(t *testing.T) {
	f := newFixture()
	roster := &fakeRoster{}
	f.opts.Roster = roster

	chatter := groupMessage("just chatting")
	chatter.From.Username = "bek"
	joined := groupMessage("")
	joined.NewChatMembers = []telegram.User{{ID: 7007, FirstName: "Said", Username: "said_05"}}
	left := groupMessage("")
	left.LeftChatMember = &telegram.User{ID: 8008, FirstName: "Gone"}
	banned := telegram.Update{UpdateID: 90, ChatMember: &telegram.ChatMemberUpdated{
		Chat:          telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup},
		NewChatMember: telegram.ChatMember{Status: "kicked", User: &telegram.User{ID: 9009, FirstName: "Spam"}},
	}}
	fromBot := groupMessage("my own line")
	fromBot.From = &telegram.User{ID: botID, IsBot: true, FirstName: "Mira"}

	f.run(inGroup(chatter), inGroup(joined), inGroup(left), banned, inGroup(fromBot), textFrom(userID, "private hi"))

	want := []storage.GroupMember{
		{ChatID: groupID, UserID: userID, FirstName: "Bek", Username: "bek"},
		{ChatID: groupID, UserID: userID, FirstName: "Bek"},
		{ChatID: groupID, UserID: 7007, FirstName: "Said", Username: "said_05", Status: "member"},
		{ChatID: groupID, UserID: userID, FirstName: "Bek"},
		{ChatID: groupID, UserID: 8008, FirstName: "Gone", Status: "left"},
		{ChatID: groupID, UserID: 9009, FirstName: "Spam", Status: "kicked"},
	}
	if got := roster.all(); !slices.Equal(got, want) {
		t.Errorf("noted %+v\nwant  %+v", got, want)
	}
}

// PRIVATE_CHATS=off keeps the bot to groups: a private message gets nothing,
// not even a model call; "owners" answers only them. Groups are unchanged.
func TestPrivateChatsCanBeTurnedOff(t *testing.T) {
	owner := textFrom(9001, "salom")
	owner.Message.From.Username = ownerUsername
	cases := map[string]struct {
		mode      string
		wantSent  int
		wantCalls int
	}{
		"off":    {"off", 1, 1},    // only the group message is answered
		"owners": {"owners", 2, 2}, // the owner and the group
		"on":     {"on", 3, 3},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.OwnerUsernames = []string{ownerUsername}
			f.opts.PrivateChats = c.mode
			sent := f.run(textFrom(userID, "salom"), owner, inGroup(groupMessage("Mira, salom")))
			if len(sent) != c.wantSent || len(f.replier.recorded()) != c.wantCalls {
				t.Errorf("sent %d, model calls %d; want %d and %d", len(sent), len(f.replier.recorded()), c.wantSent, c.wantCalls)
			}
		})
	}
}
