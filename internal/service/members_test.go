package service

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

type fakeProfileBook map[int64]storage.UserProfile

func (f fakeProfileBook) Profile(_ context.Context, userID int64) (storage.UserProfile, error) {
	p := f[userID]
	p.UserID = userID
	return p, nil
}

// What a person asked to be called, and "siz", reach every turn of theirs,
// in any group; no one else's turn carries it.
func TestProfileReachesThePersonsTurns(t *testing.T) {
	f := newFixture()
	f.opts.Profiles = fakeProfileBook{userID: {CallName: "Aziz aka", Siz: true}}
	other := groupMessage("Mira, salom")
	other.From = &telegram.User{ID: 4242, FirstName: "Ann"}

	f.run(inGroup(groupMessage("Mira, salom")), inGroup(other))

	calls := f.replier.recorded()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d", len(calls))
	}
	want := `[In every chat, the person writing is to be called "Aziz aka" and spoken to with "siz".]`
	if !strings.Contains(calls[0].req.Context, want) {
		t.Errorf("context = %q, want %q", calls[0].req.Context, want)
	}
	if strings.Contains(calls[1].req.Context, "Aziz aka") || strings.Contains(calls[1].req.Context, "In every chat, the person writing") {
		t.Errorf("another member's turn carries the profile: %q", calls[1].req.Context)
	}
}

type fakeExchanges struct{ list []storage.Exchange }

func (f fakeExchanges) ExchangesElsewhere(context.Context, int64, int64, time.Time, int) ([]storage.Exchange, error) {
	return f.list, nil
}

type fakeCounter struct {
	count int
	asked int
}

func (f *fakeCounter) GetChatMemberCount(context.Context, int64) (int, error) {
	f.asked++
	return f.count, nil
}

// Where only the person and the bot can read the chat, their talks with her
// elsewhere come along; in a group with others they never do.
func TestTalksElsewhereComeOnlyWhereNobodyElseReads(t *testing.T) {
	talk := []storage.Exchange{{ChatID: -100777, ChatTitle: "Do'stlar", Asked: "Bek: qiz bilan qanday yarashsa bo'ladi", Answer: "Aybingni tan ol", At: headerNow}}
	cases := map[string]struct {
		members  int
		list     []storage.Exchange
		want     bool
		wantAsks int
	}{
		"two members: the person and the bot": {2, talk, true, 1},
		"a group with others":                 {3, talk, false, 1},
		"nothing elsewhere":                   {2, nil, false, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			counter := &fakeCounter{count: c.members}
			f.opts.Exchanges = fakeExchanges{c.list}
			f.opts.MemberCount = counter
			f.run(inGroup(groupMessage("Mira, esingdami narigi gruppadagi gaplar")))
			ctx := onlyRequest(t, f).req.Context
			got := strings.Contains(ctx, "qiz bilan qanday yarashsa") && strings.Contains(ctx, "«Do'stlar»") && strings.Contains(ctx, "Aybingni tan ol")
			if got != c.want || counter.asked != c.wantAsks {
				t.Errorf("recalled = %v (asked count %d times), want %v (%d)\ncontext: %q", got, counter.asked, c.want, c.wantAsks, ctx)
			}
		})
	}
}

// In a group of just the person and the bot nobody else is flooded, so the
// group's reply limit does not cut a conversation short there; it once left
// someone in distress unanswered. Other groups keep it.
func TestReplyLimitSparesAGroupOfTwo(t *testing.T) {
	for name, c := range map[string]struct {
		members, want int
	}{"two members": {2, 3}, "a bigger group": {5, 1}} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.GroupReplies = &fakeGroupQuota{}
			f.opts.GroupRatePerMinute = 1
			f.opts.MemberRepliesPer10Min = 1
			f.opts.MemberCount = &fakeCounter{count: c.members}
			sent := f.run(inGroup(groupMessage("Mira, bir")), inGroup(groupMessage("Mira, ikki")), inGroup(groupMessage("Mira, uch")))
			if len(sent) != c.want {
				t.Errorf("answers = %d, want %d", len(sent), c.want)
			}
		})
	}
}

// A group of the person and the bot alone is a private chat in all but
// name, so it is answered only as private chats are: with PRIVATE_CHATS=owners
// a member there gets nothing, an owner an answer, and a real group everyone.
func TestTooSmallAGroupIsAnsweredAsAPrivateChat(t *testing.T) {
	for name, c := range map[string]struct {
		mode    string
		members int
		owner   bool
		want    int
	}{
		"member, two in the group, owners only":   {"owners", 2, false, 0},
		"owner, two in the group, owners only":    {"owners", 2, true, 1},
		"member, three in the group, owners only": {"owners", 3, false, 1},
		"member, two in the group, private on":    {"on", 2, false, 1},
		"member, two in the group, private off":   {"off", 2, false, 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.PrivateChats = c.mode
			f.opts.OwnerUsernames = []string{ownerUsername}
			f.opts.MemberCount = &fakeCounter{count: c.members}
			msg := groupMessage("Mira, salom")
			if c.owner {
				fromOwner(msg)
			}
			if sent := f.run(inGroup(msg)); len(sent) != c.want {
				t.Errorf("answers = %d, want %d", len(sent), c.want)
			}
		})
	}
}
