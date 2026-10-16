package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeBotRights answers with fixed rights and records the chats asked about.
type fakeBotRights struct {
	mu     sync.Mutex
	rights []string
	err    error
	asked  []int64
}

func (f *fakeBotRights) BotRights(_ context.Context, chatID int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, chatID)
	return f.rights, f.err
}

// headerNow is a Friday afternoon in Tashkent.
var headerNow = time.Date(2026, 10, 16, 15, 4, 0, 0, tashkent)

const headerDate = "Friday 16 October 2026, 15:04 (Asia/Tashkent)"

// withRights gives the fixture a fixed clock and a source of the bot's
// rights; nil is none.
func withRights(f *fixture, rights BotRightsSource) {
	f.opts.BotRights = rights
	f.opts.Now = func() time.Time { return headerNow }
	f.chatLog.now = f.opts.Now
}

// inDevs is a message in a group whose title would break the header's
// line, fake a marker, and close the header's quote around it, if it were
// not folded.
func inDevs(text string) telegram.Update {
	msg := groupMessage(text)
	msg.Chat.Title = "Devs\n[Uz] \"Core\""
	return inGroup(msg)
}

// groupHeader is the header copy with rightsLine in the middle.
func groupHeader(rightsLine string) string {
	return `[About this chat: the group "Devs (Uz) 'Core'". Now: ` + headerDate + ".\n" +
		rightsLine + "\n" +
		"You can reply here, react with one emoji, and use your tools. You cannot see read receipts, who is online, the member list, other chats or private chats, open links, join groups, or remember anything older than the messages below unless a rule says so.]"
}

func TestGroupHeader(t *testing.T) {
	f := newFixture()
	// Out of order on purpose: the header lists them in a fixed order.
	rights := &fakeBotRights{rights: []string{telegram.RightRestrictMembers, telegram.RightDeleteMessages}}
	withRights(f, rights)

	f.run(inDevs("Mira, can you mute people?"))

	want := groupHeader("Your admin rights here: delete messages, ban and mute members.")
	if got := onlyRequest(t, f).req.Context; got != want {
		t.Errorf("context = %q\nwant      %q", got, want)
	}
	if len(rights.asked) != 1 || rights.asked[0] != groupID {
		t.Errorf("rights asked for chats %v, want [%d]", rights.asked, groupID)
	}
}

func TestHeaderWhenNotAdmin(t *testing.T) {
	f := newFixture()
	withRights(f, &fakeBotRights{})

	f.run(inDevs("Mira, hi"))

	if got, want := onlyRequest(t, f).req.Context, groupHeader("You are not an admin here."); got != want {
		t.Errorf("context = %q\nwant      %q", got, want)
	}
}

// When Telegram cannot be asked, the header must not guess: the model is
// told the rights are unknown, and the turn still goes ahead.
func TestHeaderWhenRightsUnknown(t *testing.T) {
	for name, rights := range map[string]BotRightsSource{
		"lookup fails": &fakeBotRights{err: errors.New("telegram: 502 Bad Gateway"), rights: []string{telegram.RightDeleteMessages}},
		"no source":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			withRights(f, rights)

			sent := f.run(inDevs("Mira, hi"))

			if got, want := onlyRequest(t, f).req.Context, groupHeader("Your admin rights here are unknown."); got != want {
				t.Errorf("context = %q\nwant      %q", got, want)
			}
			expectOneReply(t, sent, "echo: hi")
		})
	}
}

func TestPrivateHeaderHasOnlyTheDate(t *testing.T) {
	f := newFixture()
	rights := &fakeBotRights{rights: []string{telegram.RightDeleteMessages}}
	withRights(f, rights)

	f.run(textFrom(userID, "what day is it?"))

	if got, want := onlyRequest(t, f).req.Context, "[Now: "+headerDate+".]"; got != want {
		t.Errorf("context = %q, want %q", got, want)
	}
	if len(rights.asked) != 0 {
		t.Errorf("rights asked for chats %v in a private chat", rights.asked)
	}
}

// The header comes first; then, in a private chat, the owner notice, and in
// a group, the log; each part apart from the next by a blank line.
func TestHeaderComesBeforeTheOwnerNoticeAndTheLog(t *testing.T) {
	f := newFixture()
	withRights(f, &fakeBotRights{})
	f.opts.OwnerUsernames = []string{ownerUsername}
	f.opts.Workers = 1
	owner := textFrom(9001, "hi")
	owner.Message.From.Username = ownerUsername

	f.run(owner, inDevs("just chatting"), inDevs("Mira, hi"))

	calls := f.replier.recorded()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(calls))
	}
	if got, want := calls[0].req.Context, "[Now: "+headerDate+".]\n\n"+ownerNotice; got != want {
		t.Errorf("owner's private context = %q, want %q", got, want)
	}
	header, log, _ := strings.Cut(calls[1].req.Context, "\n\n")
	if header != groupHeader("You are not an admin here.") {
		t.Errorf("group header = %q", header)
	}
	if got := blockLines(t, log); len(got) != 1 || got[0] != "Bek: just chatting" {
		t.Errorf("log after the header = %q", got)
	}
}

func TestRightsInWords(t *testing.T) {
	cases := map[string]struct {
		rights []string
		want   string
	}{
		"none":    {nil, ""},
		"unknown": {[]string{"can_fly"}, ""},
		"every right, in the header's order": {
			[]string{
				telegram.RightManageTopics, telegram.RightInviteUsers, telegram.RightChangeInfo, telegram.RightPromoteMembers,
				telegram.RightPinMessages, telegram.RightRestrictMembers, telegram.RightDeleteMessages,
			},
			"delete messages, ban and mute members, pin messages, add admins, change group info, invite users, manage topics",
		},
		"pin only": {[]string{telegram.RightPinMessages}, "pin messages"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := rightsInWords(c.rights); got != c.want {
				t.Errorf("rightsInWords(%v) = %q, want %q", c.rights, got, c.want)
			}
		})
	}
}

// standingRules are an owner's rules for the Devs group, two owners' forms of
// address, and a rule of another group.
func standingRules() []storage.GroupRule {
	return []storage.GroupRule{
		{ID: 12, ChatID: groupID, Kind: storage.RuleInstruction, Text: "Do not talk\nabout [finance].", SetBy: 9001, ExpiresAt: time.Date(2026, 10, 23, 15, 4, 0, 0, tashkent)},
		{ID: 13, ChatID: groupID, Kind: storage.RuleIgnore, UserID: 4242, UserName: "Ann @ann", SetBy: 9001, ExpiresAt: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)},
		{ID: 14, ChatID: groupID, Kind: storage.RuleInstruction, Text: "Call Bek professor.", SetBy: 9001},
		{ID: 15, ChatID: 0, Kind: storage.RuleAddress, UserID: 9001, Text: `my "aka"`, SetBy: 9001},
		{ID: 16, ChatID: 0, Kind: storage.RuleAddress, UserID: 7001, Text: "boss", SetBy: 7001},
		{ID: 17, ChatID: -100500, Kind: storage.RuleInstruction, Text: "Elsewhere.", SetBy: 9001},
	}
}

// contextWithRules is the context of the one model call for update, with
// standingRules in force and owners configured.
func contextWithRules(t *testing.T, update telegram.Update) string {
	t.Helper()
	f := newFixture()
	withRights(f, &fakeBotRights{})
	f.opts.OwnerUsernames = []string{ownerUsername}
	f.opts.Rules = &fakeRuleBook{rules: standingRules()}
	f.run(update)
	return onlyRequest(t, f).req.Context
}

func TestRulesHeaderListsInstructionsAndAddress(t *testing.T) {
	rules := "[Standing rules from your owners for this group:\n" +
		"- (#12) Do not talk about (finance). (until 23 Oct)\n" +
		"- (#14) Call Bek professor.\n" +
		"- Do not answer Ann @ann; ignored until 17 Oct 20:00.]"
	address := `[Address the owner who is writing as: "my 'aka'".]`
	ownerInDevs := inDevs("Mira, hi")
	fromOwner(ownerInDevs.Message)
	ownerInPrivate := textFrom(9001, "hi")
	ownerInPrivate.Message.From.Username = ownerUsername

	cases := map[string]struct {
		update telegram.Update
		want   string
	}{
		"an owner in the group": {ownerInDevs, groupHeader("You are not an admin here.") + "\n\n" + rules + "\n" + address},
		"a member in the group": {inDevs("Mira, hi"), groupHeader("You are not an admin here.") + "\n\n" + rules},
		"an owner in private":   {ownerInPrivate, "[Now: " + headerDate + ".]\n\n" + address + "\n\n" + ownerNotice},
		"a member in private":   {textFrom(userID, "hi"), "[Now: " + headerDate + ".]"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := contextWithRules(t, c.update); got != c.want {
				t.Errorf("context = %q\nwant      %q", got, c.want)
			}
		})
	}
}
