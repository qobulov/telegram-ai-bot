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

	// Bek is noted again only when his name changed: the third and fourth
	// messages show him as the second did, so he is not written again.
	want := []storage.GroupMember{
		{ChatID: groupID, UserID: userID, FirstName: "Bek", Username: "bek"},
		{ChatID: groupID, UserID: userID, FirstName: "Bek"},
		{ChatID: groupID, UserID: 7007, FirstName: "Said", Username: "said_05", Status: "member"},
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
		mode          string
		wantSent      int
		wantCalls     int
		wantReactions int
	}{
		"off":    {"off", 1, 1, 2},    // only the group message is answered
		"owners": {"owners", 2, 2, 1}, // the owner and the group
		"on":     {"on", 3, 3, 0},
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
			// Who is not answered gets a reaction, chosen in code.
			reactions := f.reactor.recorded()
			if len(reactions) != c.wantReactions {
				t.Errorf("reactions = %+v, want %d", reactions, c.wantReactions)
			}
			for _, r := range reactions {
				if !slices.Contains(privateReactions, r.emoji) {
					t.Errorf("reaction %q is not one of privateReactions", r.emoji)
				}
			}
		})
	}
}

func TestPrivateReactionsAreAllowedByTelegram(t *testing.T) {
	for _, e := range privateReactions {
		if !telegram.IsReactionEmoji(e) {
			t.Errorf("%q is not a reaction Telegram accepts", e)
		}
	}
}

// A flood of private messages from someone not answered gets no reaction
// past the flood guard, and never the guard's text notice.
func TestUnansweredPrivateFloodGetsNothing(t *testing.T) {
	f := newFixture()
	f.opts.PrivateChats = "owners"
	var updates []telegram.Update
	for i := range 30 {
		u := textFrom(userID, "salom")
		u.UpdateID += int64(i)
		u.Message.MessageID += int64(i)
		updates = append(updates, u)
	}
	sent := f.run(updates...)
	if len(sent) != 0 || len(f.replier.recorded()) != 0 {
		t.Errorf("sent %d (%+v), model calls %d; want none", len(sent), sent, len(f.replier.recorded()))
	}
	if n := len(f.reactor.recorded()); n == 0 || n == 30 {
		t.Errorf("reactions = %d, want some, cut by the flood guard", n)
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
	want := `[In every chat, the person writing is to be called "Aziz aka".]`
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

// A group of fewer than six members (more than five are needed) is answered
// only as private chats are: with PRIVATE_CHATS=owners a member there gets a
// 👀 reaction and no answer, an owner an answer, and a group of six or more
// everyone.
func TestTooSmallAGroupIsAnsweredAsAPrivateChat(t *testing.T) {
	for name, c := range map[string]struct {
		mode    string
		members int
		owner   bool
		want    int
	}{
		"member, two in the group, owners only":  {"owners", 2, false, 0},
		"owner, two in the group, owners only":   {"owners", 2, true, 1},
		"member, five in the group, owners only": {"owners", 5, false, 0},
		"member, six in the group, owners only":  {"owners", 6, false, 1},
		"member, two in the group, private on":   {"on", 2, false, 1},
		"member, two in the group, private off":  {"off", 2, false, 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.PrivateChats = c.mode
			f.opts.MinGroupMembers = 6
			f.opts.OwnerUsernames = []string{ownerUsername}
			f.opts.MemberCount = &fakeCounter{count: c.members}
			msg := groupMessage("Mira, salom")
			if c.owner {
				fromOwner(msg)
			}
			if sent := f.run(inGroup(msg)); len(sent) != c.want {
				t.Errorf("answers = %d, want %d", len(sent), c.want)
			}
			reactions := f.reactor.recorded()
			if unanswered := c.want == 0; unanswered != (len(reactions) == 1 && reactions[0].emoji == overQuotaReaction) {
				t.Errorf("reactions = %+v with %d answers; want 👀 exactly when unanswered", reactions, c.want)
			}
			if c.want == 0 && len(f.replier.recorded()) != 0 {
				t.Error("the model was asked")
			}
		})
	}
}

type fakeBotTally struct{ bots int }

func (f fakeBotTally) OtherBots(context.Context, int64, int64) (int, error) { return f.bots, nil }

// In production a member added a music bot to a group of five to reach six;
// the bots seen in a group do not count as members.
func TestBotsDoNotMakeAGroupBigEnough(t *testing.T) {
	for name, c := range map[string]struct {
		members, bots, want int
	}{"six with a bot": {6, 1, 0}, "seven with a bot": {7, 1, 1}, "six, no bots": {6, 0, 1}} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.PrivateChats = "owners"
			f.opts.MinGroupMembers = 6
			f.opts.MemberCount = &fakeCounter{count: c.members}
			f.opts.Bots = fakeBotTally{bots: c.bots}
			if sent := f.run(inGroup(groupMessage("Mira, salom"))); len(sent) != c.want {
				t.Errorf("answers = %d, want %d", len(sent), c.want)
			}
		})
	}
}

// GROUP_MIN_MEMBERS sets the size from Vercel: 3 lets a group of three in,
// and 0 answers groups of any size.
func TestTheGroupSizeLimitIsASetting(t *testing.T) {
	for name, c := range map[string]struct{ min, members, want int }{
		"three of three": {3, 3, 1}, "two of three": {3, 2, 0}, "no minimum": {0, 2, 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.PrivateChats = "owners"
			f.opts.MinGroupMembers = c.min
			f.opts.MemberCount = &fakeCounter{count: c.members}
			if sent := f.run(inGroup(groupMessage("Mira, salom"))); len(sent) != c.want {
				t.Errorf("answers = %d, want %d", len(sent), c.want)
			}
		})
	}
}

// Someone the swear guard caught swearing at the bot in this chat is to be
// spoken to with "sen"; anyone else keeps the persona's "siz".
func TestWhoSworeAtTheBotGetsSen(t *testing.T) {
	cases := map[string]struct {
		owner   bool
		strikes []storage.ModerationAction
		want    bool
	}{
		"a member": {},
		"an owner": {owner: true},
		"swore at the bot here": {
			strikes: []storage.ModerationAction{{ChatID: groupID, TargetUserID: userID, Action: storage.ActionSwearAtBot}},
			want:    true,
		},
		"swore at the bot elsewhere": {
			strikes: []storage.ModerationAction{{ChatID: -100999, TargetUserID: userID, Action: storage.ActionSwearAtBot}},
		},
		"swore at a person": {
			strikes: []storage.ModerationAction{{ChatID: groupID, TargetUserID: userID, Action: storage.ActionSwear}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.opts.Strikes = &fakeStrikes{actions: c.strikes}
			if c.owner {
				f.opts.OwnerIDs = map[int64]bool{userID: true}
			}

			f.run(inGroup(groupMessage("Mira, salom")))

			calls := f.replier.recorded()
			if len(calls) != 1 {
				t.Fatalf("model calls = %d", len(calls))
			}
			if got := strings.Contains(calls[0].req.Context, senLine); got != c.want {
				t.Errorf("sen line = %v, want %v; context %q", got, c.want, calls[0].req.Context)
			}
		})
	}
}

// An owner told to reply to a member's message with an order often replies
// with only the bot's name: their own messages of the last minutes count as
// their words, so the order can be saved. A member's never do.
func TestOwnersEarlierMessagesCountAsTheirWords(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	order := "Bekni master deb chaqir"

	first, second := fromOwner(groupMessage(order)), fromOwner(replyingTo(groupMessage("Mira"), userID, "salom"))
	first.MessageID, second.MessageID = 41, 42

	f.run(inGroup(first), inGroup(second), inGroup(groupMessage("Mira, salom")))

	calls := f.replier.recorded()
	if len(calls) < 2 {
		t.Fatalf("model calls = %d", len(calls))
	}
	owner, member := calls[len(calls)-2].caller, calls[len(calls)-1].caller
	if !strings.Contains(owner.Words, order) || !strings.Contains(owner.Words, "Mira") {
		t.Errorf("owner's words = %q, want the earlier order with this message", owner.Words)
	}
	if strings.Contains(member.Words, order) {
		t.Errorf("a member's words carry the owner's order: %q", member.Words)
	}
}

// An answer the model meant for the member the asker replied to goes as a
// reply to that member's message; never to the bot's own or the asker's.
func TestToThemAnswerRepliesToTheMember(t *testing.T) {
	cases := map[string]struct {
		repliedTo int64
		want      int64
	}{
		"a member's message": {repliedTo: 4242, want: 29},
		"the bot's message":  {repliedTo: botID, want: 30},
		"the asker's own":    {repliedTo: userID, want: 30},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.replier.answer, f.replier.toReplied = "Miyang joyida.", true
			ask := replyingTo(groupMessage("Mira, bunga bir nima de"), c.repliedTo, "miyasi kichkina")

			sent := f.run(inGroup(ask))

			if len(sent) != 1 || sent[0].replyTo != c.want {
				t.Errorf("sent = %+v, want one reply to %d", sent, c.want)
			}
		})
	}
}

// Asked, in reply to the bot's answer, to say something to the member it
// answered, the bot replies to that member's message.
func TestToThemAnswerInReplyToTheBotGoesToWhomItAnswered(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{ownerUsername}
	answersTo29(f)
	f.replier.answer, f.replier.toReplied = "Nechanchi yilsiz?", true

	sent := f.run(inGroup(fromOwner(replyingTo(groupMessage("Mira, undan yilini so'ra"), botID, "salom Bek"))))

	if len(sent) != 1 || sent[0].replyTo != 28 {
		t.Errorf("sent = %+v, want one reply to Bek's message 28", sent)
	}
}

// A group message that replies to another member names that member for the
// model, with when to use [to them]; one replying to the bot or to oneself
// does not.
func TestToThemHintNamesTheRepliedMember(t *testing.T) {
	f := newFixture()
	toMember := replyingTo(groupMessage("Mira, nimadir de"), 4242, "Дэ")
	toMember.ReplyToMessage.From.FirstName = "Patrick"
	toBot := replyingTo(groupMessage("Mira, nimadir de"), botID, "salom")
	toBot.MessageID = 31

	f.run(inGroup(toMember), inGroup(toBot))

	calls := f.replier.recorded()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d", len(calls))
	}
	if ctx := calls[0].req.Context; !strings.Contains(ctx, "is replying to a message from Patrick") || !strings.Contains(ctx, "start with [to them] and speak to Patrick") {
		t.Errorf("context = %q, want Patrick named", ctx)
	}
	if strings.Contains(calls[1].req.Context, "[to them]") {
		t.Errorf("a reply to the bot carries the hint: %q", calls[1].req.Context)
	}
}
