package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeChat answers getChatMember as Telegram would: anyone it was not told
// about is a plain member.
type fakeChat struct {
	mu      sync.Mutex
	members map[int64]telegram.ChatMember
	// failing are the users whose lookup fails.
	failing map[int64]bool
	asked   []int64
}

func (f *fakeChat) GetChatMember(_ context.Context, _, userID int64) (telegram.ChatMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, userID)
	if f.failing[userID] {
		return telegram.ChatMember{}, errors.New("telegram: 502 Bad Gateway")
	}
	if member, ok := f.members[userID]; ok {
		return member, nil
	}
	return telegram.ChatMember{Status: "member"}, nil
}

func (f *fakeChat) set(userID int64, member telegram.ChatMember) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[userID] = member
}

func (f *fakeChat) lookups() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.asked...)
}

// fakeTelegram records the moderation calls it is asked to make.
type fakeTelegram struct {
	mu           sync.Mutex
	calls        []string
	restrictions []telegram.Restriction
	err          error
}

func (f *fakeTelegram) do(method string, chatID, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s %d %d", method, chatID, id))
	return f.err
}

func (f *fakeTelegram) DeleteMessage(_ context.Context, chatID, messageID int64) error {
	return f.do("deleteMessage", chatID, messageID)
}

func (f *fakeTelegram) RestrictChatMember(_ context.Context, r telegram.Restriction) error {
	f.mu.Lock()
	f.restrictions = append(f.restrictions, r)
	f.mu.Unlock()
	return f.do("restrictChatMember", r.ChatID, r.UserID)
}

func (f *fakeTelegram) BanChatMember(_ context.Context, chatID, userID int64) error {
	return f.do("banChatMember", chatID, userID)
}

func (f *fakeTelegram) UnbanChatMember(_ context.Context, chatID, userID int64) error {
	return f.do("unbanChatMember", chatID, userID)
}

func (f *fakeTelegram) PinChatMessage(_ context.Context, chatID, messageID int64) error {
	return f.do("pinChatMessage", chatID, messageID)
}

func (f *fakeTelegram) UnpinChatMessage(_ context.Context, chatID, messageID int64) error {
	return f.do("unpinChatMessage", chatID, messageID)
}

func (f *fakeTelegram) made() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeTelegram) restricted() []telegram.Restriction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.Restriction(nil), f.restrictions...)
}

// fakeAudit keeps the audit in memory the way the store does, without the
// times: every action counts as recent.
type fakeAudit struct {
	mu      sync.Mutex
	actions []storage.ModerationAction
	// since is what the tool asked the store for.
	since []time.Time
	// forgetErr makes ForgetModeration fail and keep the record; countErr
	// makes ModerationBy fail.
	forgetErr, countErr error
}

func (f *fakeAudit) RecordModeration(_ context.Context, a storage.ModerationAction) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.ContainsFunc(f.actions, func(old storage.ModerationAction) bool {
		return old.UpdateID == a.UpdateID && old.Action == a.Action
	}) {
		return false, nil
	}
	f.actions = append(f.actions, a)
	return true, nil
}

func (f *fakeAudit) ForgetModeration(_ context.Context, updateID int64, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forgetErr != nil {
		return f.forgetErr
	}
	f.actions = slices.DeleteFunc(f.actions, func(a storage.ModerationAction) bool {
		return a.UpdateID == updateID && a.Action == action
	})
	return nil
}

func (f *fakeAudit) ModerationBy(_ context.Context, requesterID int64, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countErr != nil {
		return 0, f.countErr
	}
	f.since = append(f.since, since)
	count := 0
	for _, a := range f.actions {
		if a.RequesterID == requesterID && a.Action != "poll" {
			count++
		}
	}
	return count, nil
}

func (f *fakeAudit) recorded() []storage.ModerationAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storage.ModerationAction(nil), f.actions...)
}

// fakeRightsBook records what the tool learned about the bot's rights.
type fakeRightsBook struct {
	mu    sync.Mutex
	saved []string
}

func (f *fakeRightsBook) SetBotRights(_ context.Context, chatID int64, status string, rights []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, fmt.Sprintf("%d %s %v", chatID, status, rights))
	return nil
}

func (f *fakeRightsBook) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.saved...)
}

// admin is an administrator holding the given rights.
func admin(rights ...string) telegram.ChatMember {
	member := telegram.ChatMember{Status: "administrator"}
	for _, right := range rights {
		switch right {
		case telegram.RightDeleteMessages:
			member.CanDeleteMessages = true
		case telegram.RightRestrictMembers:
			member.CanRestrictMembers = true
		case telegram.RightPinMessages:
			member.CanPinMessages = true
		}
	}
	return member
}

var moderationRights = []string{telegram.RightDeleteMessages, telegram.RightRestrictMembers, telegram.RightPinMessages}

type moderationEnv struct {
	chat       *fakeChat
	telegram   *fakeTelegram
	audit      *fakeAudit
	rights     *fakeRightsBook
	opts       ModerationOptions
	moderation *Moderation
	registry   *Registry
}

// newModerationEnv is groupA, where the bot and the person asking (userID)
// are admins holding every right moderation uses, and Bek (memberID) is a
// member.
func newModerationEnv(t *testing.T) *moderationEnv {
	t.Helper()
	env := &moderationEnv{
		chat: &fakeChat{
			members: map[int64]telegram.ChatMember{rulesBotID: admin(moderationRights...), userID: admin(moderationRights...)},
			failing: map[int64]bool{},
		},
		telegram: &fakeTelegram{},
		audit:    &fakeAudit{},
		rights:   &fakeRightsBook{},
	}
	env.opts = ModerationOptions{
		BotID:      rulesBotID,
		Members:    env.chat,
		Telegram:   env.telegram,
		Store:      env.audit,
		Rights:     env.rights,
		Location:   tashkent,
		Now:        func() time.Time { return rulesNow },
		OwnersByID: true,
	}
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	return env
}

func (env *moderationEnv) moderate(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "moderate", args)), &result)
	return result
}

// nothingActed fails the test if Telegram was asked to act or anything was
// recorded.
func (env *moderationEnv) nothingActed(t *testing.T) {
	t.Helper()
	if calls := env.telegram.made(); len(calls) != 0 {
		t.Errorf("Telegram was asked to %q", calls)
	}
	if actions := env.audit.recorded(); len(actions) != 0 {
		t.Errorf("recorded %+v", actions)
	}
}

// act has the tool asked for args in ctx, expects it done, and returns what
// it says was done.
func (env *moderationEnv) act(t *testing.T, ctx context.Context, args string) string {
	t.Helper()
	got := env.moderate(ctx, args)
	var result struct {
		Done bool   `json:"done"`
		Said string `json:"said"`
	}
	if !got.OK || json.Unmarshal(got.Result, &result) != nil || !result.Done {
		t.Fatalf("moderate %s = %+v %s, want it done", args, got, got.Result)
	}
	return result.Said
}

// askingForAll is a request in words for every action, so that only what a
// test is about decides whether the tool acts.
const askingForAll = "Maria, buni o'chir, mute qil, unmute, ban, unban, pin qil"

// asking is userID's turn in groupA, in update updateID, replying to target.
func asking(updateID int64, target *Target) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: 50, Target: target, Words: askingForAll}, &Sink{})
}

// ownerAsking is asking, by one of the bot's owners.
func ownerAsking(updateID int64, target *Target) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: updateID, MessageID: 50, Target: target, Words: askingForAll}, &Sink{})
}

// botMessage is a message the bot itself wrote.
func botMessage() *Target {
	return &Target{MessageID: 28, UserID: rulesBotID, Name: "Maria @maria_bot", IsBot: true, Text: "salom"}
}

func expectRefusal(t *testing.T, got envelope, code, reason string) {
	t.Helper()
	if got.OK || got.Error.Code != code || !strings.Contains(got.Error.Message, reason) {
		t.Errorf("result = %+v, want %s containing %q", got, code, reason)
	}
}

// The owners asked for no question to confirm: one call does it.
func TestModerateActsAtOnce(t *testing.T) {
	env := newModerationEnv(t)

	got := env.moderate(asking(100, bekTarget()), `{"action":"mute","minutes":10080,"reason":"spam"}`)

	// The said line is the bot's own words, and the model's reason is not
	// in it.
	want := `{"done":true,"note":"` + doneNote + `","said":"Bajarildi: Bek @bek 23.10 15:04 gacha yoza olmaydi."}`
	if !got.OK || string(got.Result) != want {
		t.Fatalf("mute = %+v %s\nwant %s", got, got.Result, want)
	}
	restrictions := env.telegram.restricted()
	if len(restrictions) != 1 {
		t.Fatalf("restrictions = %+v, want one", restrictions)
	}
	if r := restrictions[0]; r.ChatID != groupA || r.UserID != memberID || r.Permissions != telegram.NoPermissions() || !r.Until.Equal(rulesNow.Add(7*24*time.Hour)) {
		t.Errorf("restriction = %+v, want Bek muted for a week", r)
	}
	wantAudit := storage.ModerationAction{ChatID: groupA, Action: "mute", RequesterID: userID, TargetUserID: memberID, TargetMessageID: 29, Minutes: 10080, Reason: "spam", UpdateID: 100}
	if got := env.audit.recorded(); len(got) != 1 || got[0] != wantAudit {
		t.Errorf("audit = %+v\nwant    [%+v]", got, wantAudit)
	}
	if saved, want := env.rights.all(), fmt.Sprintf("%d administrator %v", groupA, moderationRights); len(saved) != 1 || saved[0] != want {
		t.Errorf("stored rights = %q, want [%q] from the one check", saved, want)
	}
}

// The model reads the whole chat, and a line in it can talk it into calling
// the tool on anyone's turn: the person's own words must ask for the action.
func TestModerateActsOnlyWhenAskedInWords(t *testing.T) {
	env := newModerationEnv(t)
	wondering := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, MessageID: 50, Target: bekTarget(), Words: "Maria, bu odam haqida nima deysan?"}, &Sink{})

	for _, action := range []string{"delete", "mute", "unmute", "ban", "unban", "pin", "unpin"} {
		expectRefusal(t, env.moderate(wondering, `{"action":"`+action+`"}`), "forbidden", "does not ask for this")
	}
	env.nothingActed(t)
	// It is refused before anyone's rights are looked up.
	if asked := env.chat.lookups(); len(asked) != 0 {
		t.Errorf("looked up %v, want nobody", asked)
	}

	cases := []struct {
		words, action string
		want          bool
	}{
		{"Maria, buni o'chir", "delete", true},
		{"oʻchirib tashla", "delete", true},
		{"mute qilvor 10 daqiqaga", "mute", true},
		{"jim qil buni", "mute", true},
		{"заблокируй его", "ban", true},
		{"bloklab tashla", "ban", true},
		{"pin qil", "pin", true},
		{"закрепи", "pin", true},
		{"mutedan chiqar", "unmute", true},
		{"salom", "mute", false},
		{"pin qil", "delete", false},
	}
	for _, c := range cases {
		if got := askedFor(c.words, c.action); got != c.want {
			t.Errorf("askedFor(%q, %s) = %t, want %t", c.words, c.action, got, c.want)
		}
	}
}

func TestModerateActions(t *testing.T) {
	cases := []struct {
		action, call string
		// done is in Uzbek, then in Russian; only the Uzbek is said.
		done [2]string
		// text is what the audit keeps of the message.
		text string
	}{
		{"delete", "deleteMessage -100111 29",
			[2]string{"Bajarildi: Bek @bek xabari o'chirildi.", "Готово: сообщение от Bek @bek удалено."}, "you're slow"},
		{"unmute", "restrictChatMember -100111 5005",
			[2]string{"Bajarildi: Bek @bek yana yoza oladi.", "Готово: Bek @bek снова может писать."}, ""},
		{"ban", "banChatMember -100111 5005",
			[2]string{"Bajarildi: Bek @bek bloklandi.", "Готово: Bek @bek забанен(а)."}, ""},
		{"unban", "unbanChatMember -100111 5005",
			[2]string{"Bajarildi: Bek @bek blokdan chiqarildi va qayta qo'shila oladi.", "Готово: Bek @bek разбанен(а) и может вернуться."}, ""},
		{"pin", "pinChatMessage -100111 29",
			[2]string{"Bajarildi: Bek @bek xabari qadaldi.", "Готово: сообщение от Bek @bek закреплено."}, ""},
		{"unpin", "unpinChatMessage -100111 29",
			[2]string{"Bajarildi: Bek @bek xabari qadashdan olindi.", "Готово: сообщение от Bek @bek откреплено."}, ""},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			env := newModerationEnv(t)

			said := env.act(t, asking(100, bekTarget()), `{"action":"`+c.action+`"}`)

			if said != c.done[0] {
				t.Errorf("said = %q, want %q", said, c.done[0])
			}
			if calls := env.telegram.made(); len(calls) != 1 || calls[0] != c.call {
				t.Errorf("calls = %q, want [%q]", calls, c.call)
			}
			if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != c.action || audit[0].TargetText != c.text || audit[0].Minutes != 0 || audit[0].UpdateID != 100 {
				t.Errorf("audit = %+v, want %s with text %q in the requesting update", audit, c.action, c.text)
			}
		})
	}

	// Unmuting lifts every restriction, for good.
	env := newModerationEnv(t)
	env.act(t, asking(100, bekTarget()), `{"action":"unmute"}`)
	if r := env.telegram.restricted(); len(r) != 1 || r[0].Permissions != telegram.AllPermissions() || !r[0].Until.IsZero() {
		t.Errorf("unmute restriction = %+v, want every permission back", r)
	}
}

func TestModerateRefusesAdminWithoutTheRight(t *testing.T) {
	for name, requester := range map[string]telegram.ChatMember{
		"an admin who may only delete": admin(telegram.RightDeleteMessages),
		"a member":                     {Status: "member"},
		// A restricted member's Can fields are permissions, not rights.
		"a restricted member": {Status: "restricted", CanRestrictMembers: true},
	} {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			env.chat.set(userID, requester)

			got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`)

			expectRefusal(t, got, "forbidden", "only admins of this group who have the right to ban and mute members, or my owners, can ask me to do that; the person asking is neither")
			env.nothingActed(t)
			if asked := env.chat.lookups(); !slices.Equal(asked, []int64{userID}) {
				t.Errorf("looked up %v, want only the person asking", asked)
			}
		})
	}
}

func TestModerateOwnerMayActWithoutBeingAdmin(t *testing.T) {
	env := newModerationEnv(t)
	env.chat.set(userID, telegram.ChatMember{Status: "member"})

	if said := env.act(t, ownerAsking(100, bekTarget()), `{"action":"delete"}`); !strings.HasPrefix(said, "Bajarildi:") {
		t.Fatalf("delete by an owner said %q", said)
	}
	if calls := env.telegram.made(); !slices.Equal(calls, []string{"deleteMessage -100111 29"}) {
		t.Errorf("calls = %q", calls)
	}
	if slices.Contains(env.chat.lookups(), userID) {
		t.Error("the owner's own rights were looked up")
	}
}

// Without BOT_OWNER_IDS an owner is known only by a username, which can be
// given up and registered by someone else: that is no ground to moderate
// any group, nor to be spared.
func TestModerateOwnerByUsernameIsAnOrdinaryMember(t *testing.T) {
	env := newModerationEnv(t)
	env.opts.OwnersByID = false
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	env.chat.set(userID, telegram.ChatMember{Status: "member"})

	expectRefusal(t, env.moderate(ownerAsking(100, bekTarget()), `{"action":"delete"}`), "forbidden", "only admins of this group who have the right to delete messages, or my owners, can ask me to do that; the person asking is neither")
	env.nothingActed(t)

	// An admin may have an owner's message deleted.
	env.chat.set(userID, admin(moderationRights...))
	owners := &Target{MessageID: 29, UserID: 9002, Name: "Muhammad", Owner: true}
	if said := env.act(t, asking(101, owners), `{"action":"delete"}`); !strings.HasPrefix(said, "Bajarildi:") {
		t.Errorf("deleting an owner's message said %q", said)
	}
}

func TestModerateNeedsBotRight(t *testing.T) {
	for name, bot := range map[string]telegram.ChatMember{
		"an admin who may only delete": admin(telegram.RightDeleteMessages),
		"a member":                     {Status: "member"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			env.chat.set(rulesBotID, bot)

			for _, ctx := range []context.Context{asking(100, bekTarget()), ownerAsking(101, bekTarget())} {
				expectRefusal(t, env.moderate(ctx, `{"action":"mute"}`), "forbidden", "I don't have the right to ban and mute members in this group; its admins can give it to me")
			}
			env.nothingActed(t)
			// What Telegram said is kept, for the turn header.
			if saved, want := env.rights.all(), fmt.Sprintf("%d %s %v", groupA, bot.Status, bot.Rights()); len(saved) == 0 || saved[0] != want {
				t.Errorf("stored rights = %q, want %q", saved, want)
			}
		})
	}
}

func TestModerateRefusesImmuneTargets(t *testing.T) {
	creator := telegram.ChatMember{Status: "creator"}
	noRights := admin()
	// The person asking and the bot may have their own messages deleted or
	// pinned; everyone else here is kept off even that.
	members := []string{"mute", "ban"}
	everything := []string{"mute", "ban", "delete", "pin"}
	cases := map[string]struct {
		target  *Target
		member  *telegram.ChatMember
		actions []string
	}{
		"the person asking": {&Target{MessageID: 29, UserID: userID, Name: "Ali"}, nil, members},
		"the bot":           {botMessage(), nil, members},
		"an owner":          {&Target{MessageID: 29, UserID: 9002, Name: "Muhammad", Owner: true}, nil, everything},
		"the creator":       {bekTarget(), &creator, everything},
		// An admin is immune whatever rights they hold.
		"an administrator":        {bekTarget(), &noRights, everything},
		"a linked channel's post": {&Target{MessageID: 29, UserID: 777000, Name: "Telegram"}, nil, everything},
		"an anonymous admin":      {&Target{MessageID: 29, UserID: 1087968824, Name: "Group @GroupAnonymousBot", IsBot: true}, nil, everything},
		"a channel":               {&Target{MessageID: 29, UserID: 136817688, Name: "Channel @Channel_Bot", IsBot: true}, nil, everything},
	}
	for name, c := range cases {
		for _, action := range c.actions {
			t.Run(name+" "+action, func(t *testing.T) {
				env := newModerationEnv(t)
				if c.member != nil {
					env.chat.set(c.target.UserID, *c.member)
				}

				got := env.moderate(asking(100, c.target), `{"action":"`+action+`"}`)

				expectRefusal(t, got, "forbidden", "I can't do that to")
				env.nothingActed(t)
			})
		}
	}
}

func TestModerateOwnMessageDeleteAllowedMuteRefused(t *testing.T) {
	env := newModerationEnv(t)

	if said := env.act(t, asking(100, botMessage()), `{"action":"delete"}`); !strings.HasPrefix(said, "Bajarildi:") {
		t.Fatalf("deleting the bot's own message said %q", said)
	}
	expectRefusal(t, env.moderate(asking(102, botMessage()), `{"action":"mute"}`), "forbidden", "I can't do that to")

	// The person asking may have their own message deleted or pinned, but
	// may not mute or ban themselves.
	own := &Target{MessageID: 27, UserID: userID, Name: "Ali"}
	for i, action := range []string{"delete", "pin", "unpin"} {
		if said := env.act(t, asking(int64(110+i), own), `{"action":"`+action+`"}`); !strings.HasPrefix(said, "Bajarildi:") {
			t.Errorf("%s of one's own message said %q", action, said)
		}
	}
	expectRefusal(t, env.moderate(asking(120, own), `{"action":"mute"}`), "forbidden", "I can't do that to")

	want := []string{"deleteMessage -100111 28", "deleteMessage -100111 27", "pinChatMessage -100111 27", "unpinChatMessage -100111 27"}
	if calls := env.telegram.made(); !slices.Equal(calls, want) {
		t.Errorf("calls = %q\nwant    %q", calls, want)
	}
}

func TestModerateNeedsAReplyTarget(t *testing.T) {
	env := newModerationEnv(t)

	for _, action := range []string{"delete", "mute", "unmute", "ban", "unban", "pin", "unpin"} {
		expectRefusal(t, env.moderate(asking(100, nil), `{"action":"`+action+`"}`), "forbidden", "reply to the message of the member you mean")
	}
	env.nothingActed(t)
}

// The audit record is unique per update and action, so an update Telegram
// delivers twice is acted on once.
func TestModerateSameUpdateTwiceActsOnce(t *testing.T) {
	env := newModerationEnv(t)
	env.act(t, asking(100, bekTarget()), `{"action":"mute"}`)

	if got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`); !got.OK {
		t.Errorf("mute again = %+v", got)
	}

	if calls, audit := env.telegram.made(), env.audit.recorded(); len(calls) != 1 || len(audit) != 1 {
		t.Errorf("calls = %q, audit = %+v; want one of each", calls, audit)
	}
}

func TestModerateFailsClosedWhenLookupFails(t *testing.T) {
	for name, who := range map[string]int64{"the person asking": userID, "the bot": rulesBotID, "the member": memberID} {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			env.chat.failing[who] = true

			for _, action := range []string{"delete", "mute", "ban"} {
				expectRefusal(t, env.moderate(asking(100, bekTarget()), `{"action":"`+action+`"}`), "execution_failed", "nothing was done")
			}
			env.nothingActed(t)
		})
	}
}

func TestModerateRateLimit(t *testing.T) {
	env := newModerationEnv(t)
	record := func(requesterID int64, action string, updateID int64) {
		_, _ = env.audit.RecordModeration(context.Background(), storage.ModerationAction{ChatID: groupA, Action: action, RequesterID: requesterID, UpdateID: updateID})
	}
	for i := range 9 {
		record(userID, "delete", int64(i+1))
		record(9004, "delete", int64(i+11)) // someone else's
	}

	if said := env.act(t, asking(100, bekTarget()), `{"action":"mute"}`); !strings.HasPrefix(said, "Bajarildi:") {
		t.Fatalf("the 10th action said %q", said)
	}
	expectRefusal(t, env.moderate(asking(102, bekTarget()), `{"action":"mute"}`), "forbidden", "10 actions in 10 minutes")
	// An owner is held to it too.
	expectRefusal(t, env.moderate(ownerAsking(103, bekTarget()), `{"action":"delete"}`), "forbidden", "10 actions in 10 minutes")

	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want one", calls)
	}
	for _, since := range env.audit.since {
		if !since.Equal(rulesNow.Add(-10 * time.Minute)) {
			t.Errorf("counted since %v, want 10 minutes before now", since)
		}
	}
}

func TestModerateTelegramErrorIsReported(t *testing.T) {
	failures := []struct {
		err    error
		reason string
		// kept is whether the audit keeps the action: Telegram may have
		// taken it unless it refused.
		kept bool
	}{
		{&telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}, "Telegram refused: Bad Request: message can't be deleted", false},
		// Without an answer from Telegram, or with a server's error page
		// instead, it may have acted.
		{errors.New("dial tcp: i/o timeout"), noAnswer, true},
		{&telegram.APIError{Code: 502, Description: "unreadable response from deleteMessage"}, noAnswer, true},
	}
	for _, f := range failures {
		env := newModerationEnv(t)
		env.telegram.err = f.err

		got := env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)

		expectRefusal(t, got, "forbidden", f.reason)
		if strings.Contains(got.Error.Message, "i/o timeout") || strings.Contains(got.Error.Message, "unreadable") {
			t.Errorf("the cause reached the model: %q", got.Error.Message)
		}
		audit := env.audit.recorded()
		if kept := len(audit) == 1 && audit[0].TargetText == "you're slow"; kept != f.kept {
			t.Errorf("delete failing with %v: audit = %+v, want kept %t", f.err, audit, f.kept)
		}
		// Asked again in the same update, it is tried again only when
		// Telegram surely did not act.
		env.telegram.err = nil
		env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)
		want := 2
		if f.kept {
			want = 1
		}
		if calls := env.telegram.made(); len(calls) != want {
			t.Errorf("calls after %v = %q, want %d", f.err, calls, want)
		}
	}
}

func TestModerateReportsARecordItCannotDrop(t *testing.T) {
	env := newModerationEnv(t)
	env.telegram.err = &telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}
	env.audit.forgetErr = errors.New("storage: forget moderation: connection reset")

	got := env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)

	// The audit now keeps an action never taken: that goes to the logs as
	// the tool's failure, and the model hears only that something went wrong.
	expectRefusal(t, got, "execution_failed", "something went wrong")
	if strings.Contains(got.Error.Message, "connection reset") {
		t.Errorf("the store's failure reached the model: %q", got.Error.Message)
	}
	// The record stayed, so the same update asking again does nothing more.
	env.telegram.err = nil
	env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)
	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want the one refused", calls)
	}
}

func TestModerateMinutesClamped(t *testing.T) {
	cases := []struct {
		args    string
		minutes int
		until   string
	}{
		{`{"action":"mute"}`, 60, "16:04"},
		{`{"action":"mute","minutes":0}`, 60, "16:04"},
		{`{"action":"mute","minutes":90}`, 90, "16:34"},
		{`{"action":"mute","minutes":-5}`, 1, "15:05"},
		{`{"action":"mute","minutes":22}`, 22, "15:26"},
		{`{"action":"mute","minutes":120}`, 120, "17:04"},
		{`{"action":"mute","minutes":20000}`, 10080, "23.10 15:04"},
		{`{"action":"mute","minutes":1440}`, 1440, "17.10 15:04"},
		// Past midnight the date is given too.
		{`{"action":"mute","minutes":600}`, 600, "17.10 01:04"},
		{`{"action":"mute","minutes":660}`, 660, "17.10 02:04"},
	}
	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			env := newModerationEnv(t)

			said := env.act(t, asking(100, bekTarget()), c.args)

			if want := "Bajarildi: Bek @bek " + c.until + " gacha yoza olmaydi."; said != want {
				t.Errorf("said = %q, want %q", said, want)
			}
			wantUntil := rulesNow.Add(time.Duration(c.minutes) * time.Minute)
			if r := env.telegram.restricted(); len(r) != 1 || !r[0].Until.Equal(wantUntil) {
				t.Errorf("restriction = %+v, want until %v", r, wantUntil)
			}
			if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Minutes != c.minutes {
				t.Errorf("audit = %+v, want %d minutes", audit, c.minutes)
			}
		})
	}
}

func TestModerateIsForPeopleInGroups(t *testing.T) {
	env := newModerationEnv(t)
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 100, Target: bekTarget(), Words: askingForAll}, &Sink{})
	bot := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: false, UpdateID: 101, Target: bekTarget(), Words: askingForAll}, &Sink{})

	if defs := env.registry.Definitions(private, userID); len(defs) != 0 {
		t.Errorf("offered in a private chat: %+v", defs)
	}
	expectRefusal(t, env.moderate(private, `{"action":"delete"}`), "forbidden", "group")
	expectRefusal(t, env.moderate(bot, `{"action":"delete"}`), "forbidden", "person in a group")

	// The tool checks by itself too, should it run outside the registry or
	// a turn.
	tool := env.moderation.Tool()
	for _, ctx := range []context.Context{private, context.Background()} {
		if _, err := tool.Run(ctx, json.RawMessage(`{"action":"delete"}`)); err == nil {
			t.Error("Run outside a group turn succeeded")
		}
	}
	env.nothingActed(t)
}

func TestModerateChecksItsArguments(t *testing.T) {
	env := newModerationEnv(t)
	for _, args := range []string{
		`{}`,
		`{"action":"kick"}`,
		// Nothing waits for a confirmation any more, so there is nothing to
		// cancel or confirm.
		`{"action":"cancel"}`,
		`{"action":"confirm_ban"}`,
		`{"action":"mute","user_id":5}`,
		`{"action":"delete","message_id":12}`,
		`{"action":"mute","minutes":"ten"}`,
	} {
		if got := env.moderate(asking(100, bekTarget()), args); got.OK || got.Error.Code != "invalid_arguments" {
			t.Errorf("%s = %+v, want invalid_arguments", args, got)
		}
	}
	env.nothingActed(t)

	// A long reason is kept short in the audit.
	if said := env.act(t, asking(101, bekTarget()), `{"action":"delete","reason":"`+strings.Repeat("я", 300)+`"}`); !strings.HasPrefix(said, "Bajarildi:") {
		t.Fatalf("delete with a long reason said %q", said)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || len([]rune(audit[0].Reason)) > 201 {
		t.Errorf("audit = %+v, want the reason cut", audit)
	}
}

// Any member's turn can reach the tool, so the database's errors, which
// name its host and user, must not reach the model and so the group.
func TestModerateHidesStoreErrors(t *testing.T) {
	env := newModerationEnv(t)
	env.audit.countErr = errors.New("storage: count moderation: failed to connect to `user=maria_app database=maria_prod`")

	got := env.moderate(asking(100, bekTarget()), `{"action":"ban"}`)

	if got.OK || strings.Contains(got.Error.Message, "maria_prod") || got.Error.Message != "something went wrong on my side; try again later" {
		t.Errorf("ban = %+v, want the store's error hidden", got)
	}
	env.nothingActed(t)
}

// The model calling the tool twice for one message acts once, and is told
// not to report it again.
func TestModerateSecondCallInATurnSaysAlreadyDone(t *testing.T) {
	env := newModerationEnv(t)
	env.act(t, asking(100, bekTarget()), `{"action":"mute"}`)

	got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`)

	if !got.OK || !strings.Contains(string(got.Result), "already done") || strings.Contains(string(got.Result), "said") {
		t.Errorf("second call = %+v %s", got, got.Result)
	}
	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want one", calls)
	}
}

// Stickers and GIFs are taken away for as long as asked, a day by default
// and a year at most, and given back; nothing else is taken.
func TestModerateTakesAwayAndGivesBackStickers(t *testing.T) {
	env := newModerationEnv(t)
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 300, MessageID: 50, Target: bekTarget(), Words: "Maria, stiker tashashini blokla"}, &Sink{})

	said := env.act(t, ctx, `{"action":"no_stickers"}`)

	if want := "Bajarildi: Bek @bek 17.10 15:04 gacha stiker va GIF yubora olmaydi."; said != want {
		t.Errorf("said %q, want %q", said, want)
	}
	r := env.telegram.restricted()
	if len(r) != 1 || r[0].UserID != memberID || r[0].Permissions != telegram.NoStickers() || !r[0].Until.Equal(rulesNow.Add(24*time.Hour)) {
		t.Fatalf("restrictions = %+v, want Bek's stickers taken for a day", r)
	}
	if p := r[0].Permissions; !p.CanSendMessages || !p.CanSendPhotos || p.CanSendOtherMessages {
		t.Errorf("permissions = %+v, want only stickers and GIFs taken", p)
	}

	ctx = WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 301, MessageID: 51, Target: bekTarget(), Words: "100 yilga stikerini blokla"}, &Sink{})
	said = env.act(t, ctx, `{"action":"no_stickers","minutes":52560000}`)
	if want := "Bajarildi: Bek @bek 16.10.2027 15:04 gacha stiker va GIF yubora olmaydi."; said != want {
		t.Errorf("said %q, want %q, a year at most", said, want)
	}

	ctx = WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 302, MessageID: 52, Target: bekTarget(), Words: "stikerini och"}, &Sink{})
	if said := env.act(t, ctx, `{"action":"allow_stickers"}`); said != "Bajarildi: Bek @bek yana stiker va GIF yubora oladi." {
		t.Errorf("said %q", said)
	}
	if r := env.telegram.restricted(); len(r) != 3 || r[2].Permissions != telegram.AllPermissions() || !r[2].Until.IsZero() {
		t.Errorf("restrictions = %+v, want the stickers given back", r)
	}
}

// Taking stickers needs the asker's own words to be about stickers.
func TestModerateStickersOnlyWhenAsked(t *testing.T) {
	env := newModerationEnv(t)
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 303, MessageID: 50, Target: bekTarget(), Words: "Maria, salom"}, &Sink{})

	expectRefusal(t, env.moderate(ctx, `{"action":"no_stickers"}`), "forbidden", "does not ask for this")
	env.nothingActed(t)
}
