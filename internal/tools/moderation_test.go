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

// fakeAudit keeps the audit and the pending bans in memory the way the store
// does, without the times: every action counts as recent.
type fakeAudit struct {
	mu      sync.Mutex
	actions []storage.ModerationAction
	pending map[[2]int64]storage.PendingBan
	// since and maxAges are what the tool asked the store for.
	since   []time.Time
	maxAges []time.Duration
	// forgetErr makes ForgetModeration fail and keep the record.
	forgetErr error
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

func (f *fakeAudit) ModerationRecorded(_ context.Context, updateID int64, action string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.actions, func(a storage.ModerationAction) bool {
		return a.UpdateID == updateID && a.Action == action
	}), nil
}

func (f *fakeAudit) ModerationBy(_ context.Context, requesterID int64, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	count := 0
	for _, a := range f.actions {
		if a.RequesterID == requesterID && a.Action != "poll" {
			count++
		}
	}
	return count, nil
}

func (f *fakeAudit) SavePendingBan(_ context.Context, b storage.PendingBan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[[2]int64]storage.PendingBan{}
	}
	f.pending[[2]int64{b.RequesterID, b.ChatID}] = b
	return nil
}

func (f *fakeAudit) TakePendingBan(_ context.Context, requesterID, chatID, updateID int64, maxAge time.Duration) (storage.PendingBan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.maxAges = append(f.maxAges, maxAge)
	key := [2]int64{requesterID, chatID}
	b, ok := f.pending[key]
	if !ok || b.UpdateID >= updateID {
		return storage.PendingBan{}, storage.ErrNoPendingBan
	}
	delete(f.pending, key)
	return b, nil
}

func (f *fakeAudit) CancelPendingBan(_ context.Context, requesterID, chatID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{requesterID, chatID}
	_, ok := f.pending[key]
	delete(f.pending, key)
	return ok, nil
}

func (f *fakeAudit) recorded() []storage.ModerationAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storage.ModerationAction(nil), f.actions...)
}

func (f *fakeAudit) waiting() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
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
	chat     *fakeChat
	telegram *fakeTelegram
	audit    *fakeAudit
	rights   *fakeRightsBook
	opts     ModerationOptions
	registry *Registry
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
		BotID:    rulesBotID,
		Members:  env.chat,
		Telegram: env.telegram,
		Store:    env.audit,
		Rights:   env.rights,
		Location: tashkent,
		Now:      func() time.Time { return rulesNow },
	}
	env.registry = mustRegistry(t, ModerationTool(env.opts))
	return env
}

func (env *moderationEnv) moderate(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "moderate", args)), &result)
	return result
}

// nothingDone fails the test if Telegram was asked to act or anything was
// recorded or left waiting.
func (env *moderationEnv) nothingDone(t *testing.T) {
	t.Helper()
	if calls := env.telegram.made(); len(calls) != 0 {
		t.Errorf("Telegram was asked to %q", calls)
	}
	if actions := env.audit.recorded(); len(actions) != 0 {
		t.Errorf("recorded %+v", actions)
	}
	if env.audit.waiting() != 0 {
		t.Errorf("a ban is waiting for confirmation")
	}
}

// asking is userID's turn in groupA, in update updateID, replying to target.
func asking(updateID int64, target *Target) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: 50, Target: target}, &Sink{})
}

// ownerAsking is asking, by one of the bot's owners.
func ownerAsking(updateID int64, target *Target) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: updateID, MessageID: 50, Target: target}, &Sink{})
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

func TestModerateMuteByAdminWithRight(t *testing.T) {
	env := newModerationEnv(t)

	got := env.moderate(asking(100, bekTarget()), `{"action":"mute","reason":"spam"}`)

	if want := `{"action":"mute","done":true,"target":"Bek @bek","until":"16:04"}`; !got.OK || string(got.Result) != want {
		t.Fatalf("mute = %+v %s, want %s", got, got.Result, want)
	}
	restrictions := env.telegram.restricted()
	if len(restrictions) != 1 {
		t.Fatalf("restrictions = %+v, want one", restrictions)
	}
	if r := restrictions[0]; r.ChatID != groupA || r.UserID != memberID || r.Permissions != telegram.NoPermissions() || !r.Until.Equal(rulesNow.Add(time.Hour)) {
		t.Errorf("restriction = %+v, want Bek muted for an hour", r)
	}
	want := storage.ModerationAction{ChatID: groupA, Action: "mute", RequesterID: userID, TargetUserID: memberID, TargetMessageID: 29, Minutes: 60, Reason: "spam", UpdateID: 100}
	if got := env.audit.recorded(); len(got) != 1 || got[0] != want {
		t.Errorf("audit = %+v\nwant    [%+v]", got, want)
	}
	if saved, want := env.rights.all(), fmt.Sprintf("%d administrator %v", groupA, moderationRights); len(saved) != 1 || saved[0] != want {
		t.Errorf("stored rights = %q, want [%q]", saved, want)
	}
}

func TestModerateActions(t *testing.T) {
	cases := []struct {
		action, call string
		// text is what the audit keeps of the message.
		text string
	}{
		{"delete", "deleteMessage -100111 29", "you're slow"},
		{"unmute", "restrictChatMember -100111 5005", ""},
		{"unban", "unbanChatMember -100111 5005", ""},
		{"pin", "pinChatMessage -100111 29", ""},
		{"unpin", "unpinChatMessage -100111 29", ""},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			env := newModerationEnv(t)

			got := env.moderate(asking(100, bekTarget()), `{"action":"`+c.action+`"}`)

			if want := `{"action":"` + c.action + `","done":true,"target":"Bek @bek"}`; !got.OK || string(got.Result) != want {
				t.Fatalf("%s = %+v %s, want %s", c.action, got, got.Result, want)
			}
			if calls := env.telegram.made(); len(calls) != 1 || calls[0] != c.call {
				t.Errorf("calls = %q, want [%q]", calls, c.call)
			}
			if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != c.action || audit[0].TargetText != c.text || audit[0].Minutes != 0 {
				t.Errorf("audit = %+v, want %s with text %q", audit, c.action, c.text)
			}
		})
	}

	// Unmuting lifts every restriction, for good.
	env := newModerationEnv(t)
	env.moderate(asking(100, bekTarget()), `{"action":"unmute"}`)
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

			expectRefusal(t, got, "forbidden", "only this group's admins with that right, or my owners")
			env.nothingDone(t)
			if asked := env.chat.lookups(); !slices.Equal(asked, []int64{userID}) {
				t.Errorf("looked up %v, want only the person asking", asked)
			}
		})
	}
}

func TestModerateOwnerMayActWithoutBeingAdmin(t *testing.T) {
	env := newModerationEnv(t)
	env.chat.set(userID, telegram.ChatMember{Status: "member"})

	got := env.moderate(ownerAsking(100, bekTarget()), `{"action":"delete"}`)

	if !got.OK {
		t.Fatalf("delete by an owner = %+v", got)
	}
	if calls := env.telegram.made(); !slices.Equal(calls, []string{"deleteMessage -100111 29"}) {
		t.Errorf("calls = %q", calls)
	}
	if slices.Contains(env.chat.lookups(), userID) {
		t.Error("the owner's own rights were looked up")
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
				expectRefusal(t, env.moderate(ctx, `{"action":"mute"}`), "forbidden", "I don't have the right to ban and mute members here")
			}
			env.nothingDone(t)
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
				env.nothingDone(t)
			})
		}
	}
}

func TestModerateOwnMessageDeleteAllowedMuteRefused(t *testing.T) {
	env := newModerationEnv(t)

	if got := env.moderate(asking(100, botMessage()), `{"action":"delete"}`); !got.OK {
		t.Fatalf("deleting the bot's own message = %+v", got)
	}
	expectRefusal(t, env.moderate(asking(101, botMessage()), `{"action":"mute"}`), "forbidden", "I can't do that to")

	// The person asking may have their own message deleted or pinned, but
	// may not mute or ban themselves.
	own := &Target{MessageID: 27, UserID: userID, Name: "Ali"}
	for i, action := range []string{"delete", "pin", "unpin"} {
		if got := env.moderate(asking(int64(102+i), own), `{"action":"`+action+`"}`); !got.OK {
			t.Errorf("%s of one's own message = %+v", action, got)
		}
	}
	expectRefusal(t, env.moderate(asking(105, own), `{"action":"mute"}`), "forbidden", "I can't do that to")

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
	env.nothingDone(t)
}

func TestModerateSameUpdateTwiceActsOnce(t *testing.T) {
	env := newModerationEnv(t)

	if got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`); !got.OK || !strings.Contains(string(got.Result), `"done":true`) {
		t.Errorf("mute = %+v %s", got, got.Result)
	}
	// The first delivery may still be waiting for Telegram, or fail yet, so
	// the second says nothing of how it went, nor when a mute ends.
	got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`)
	want := `{"action":"mute","already_handled":true,"note":"This message was delivered twice and the first delivery already asked for this, so nothing more was done now. Don't say whether it worked."}`
	if !got.OK || string(got.Result) != want {
		t.Errorf("mute again = %+v %s\nwant %s", got, got.Result, want)
	}

	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want one", calls)
	}
	if audit := env.audit.recorded(); len(audit) != 1 {
		t.Errorf("audit = %+v, want one", audit)
	}
	// A later message is a new request.
	env.moderate(asking(101, bekTarget()), `{"action":"mute"}`)
	if calls := env.telegram.made(); len(calls) != 2 {
		t.Errorf("calls = %q, want two", calls)
	}
}

func TestModerateBanNeedsConfirmationInALaterMessage(t *testing.T) {
	env := newModerationEnv(t)

	got := env.moderate(asking(100, bekTarget()), `{"action":"ban","reason":"spam"}`)
	want := `{"note":"Nothing is done yet. Ask the person who asked to confirm in their next message, then call confirm_ban.","pending":true,"target":"Bek @bek"}`
	if !got.OK || string(got.Result) != want {
		t.Fatalf("ban = %+v %s\nwant %s", got, got.Result, want)
	}
	// Neither the message that asked nor another admin can confirm it.
	expectRefusal(t, env.moderate(asking(100, bekTarget()), `{"action":"confirm_ban"}`), "forbidden", "no ban to confirm")
	const otherAdmin = int64(9004)
	env.chat.set(otherAdmin, admin(moderationRights...))
	other := WithTurn(context.Background(), Caller{UserID: otherAdmin, ChatID: groupA, Human: true, UpdateID: 101}, &Sink{})
	expectRefusal(t, env.moderate(other, `{"action":"confirm_ban"}`), "forbidden", "no ban to confirm")
	if calls := env.telegram.made(); len(calls) != 0 {
		t.Fatalf("banned before the confirmation: %q", calls)
	}

	// The person who asked says yes in their next message, a reply to nothing.
	got = env.moderate(asking(102, nil), `{"action":"confirm_ban"}`)

	if want := `{"action":"ban","done":true,"target":"Bek @bek"}`; !got.OK || string(got.Result) != want {
		t.Fatalf("confirm_ban = %+v %s, want %s", got, got.Result, want)
	}
	if calls := env.telegram.made(); !slices.Equal(calls, []string{"banChatMember -100111 5005"}) {
		t.Errorf("calls = %q", calls)
	}
	wantAudit := storage.ModerationAction{ChatID: groupA, Action: "ban", RequesterID: userID, TargetUserID: memberID, UpdateID: 102}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0] != wantAudit {
		t.Errorf("audit = %+v\nwant    [%+v]", audit, wantAudit)
	}
	for _, maxAge := range env.audit.maxAges {
		if maxAge != 5*time.Minute {
			t.Errorf("a ban may wait %v, want 5 minutes", maxAge)
		}
	}
	// The confirmation delivered again finds its own ban, not a refusal.
	if got := env.moderate(asking(102, nil), `{"action":"confirm_ban"}`); !got.OK || !strings.Contains(string(got.Result), `"already_handled":true`) {
		t.Errorf("confirm_ban again = %+v %s, want already handled", got, got.Result)
	}
	expectRefusal(t, env.moderate(asking(103, nil), `{"action":"confirm_ban"}`), "forbidden", "no ban to confirm")
	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want the one ban", calls)
	}
}

func TestModerateBanCanBeCancelled(t *testing.T) {
	env := newModerationEnv(t)
	env.moderate(asking(100, bekTarget()), `{"action":"ban"}`)

	if got := env.moderate(asking(101, nil), `{"action":"cancel"}`); !got.OK || string(got.Result) != `{"cancelled":true}` {
		t.Errorf("cancel = %+v %s", got, got.Result)
	}
	expectRefusal(t, env.moderate(asking(102, nil), `{"action":"confirm_ban"}`), "forbidden", "no ban to confirm")
	if got := env.moderate(asking(103, nil), `{"action":"cancel"}`); !got.OK || string(got.Result) != `{"cancelled":false}` {
		t.Errorf("cancel again = %+v %s", got, got.Result)
	}
	env.nothingDone(t)
}

func TestModerateConfirmBanRechecksRights(t *testing.T) {
	cases := map[string]struct {
		change func(env *moderationEnv)
		code   string
	}{
		"the person asking lost the right": {func(env *moderationEnv) { env.chat.set(userID, admin(telegram.RightDeleteMessages)) }, "forbidden"},
		"the bot lost the right":           {func(env *moderationEnv) { env.chat.set(rulesBotID, admin(telegram.RightPinMessages)) }, "forbidden"},
		"the member became an admin":       {func(env *moderationEnv) { env.chat.set(memberID, admin()) }, "forbidden"},
		"Telegram cannot say":              {func(env *moderationEnv) { env.chat.failing[memberID] = true }, "execution_failed"},
		"the person asking did 10 actions": {func(env *moderationEnv) {
			for i := range 10 {
				_, _ = env.audit.RecordModeration(context.Background(), storage.ModerationAction{ChatID: groupA, Action: "delete", RequesterID: userID, UpdateID: int64(i + 1)})
			}
		}, "forbidden"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			if got := env.moderate(asking(100, bekTarget()), `{"action":"ban"}`); !got.OK {
				t.Fatalf("ban = %+v", got)
			}
			c.change(env)

			got := env.moderate(asking(101, nil), `{"action":"confirm_ban"}`)

			// The ban is no longer waiting, so the refusal says to ask again.
			expectRefusal(t, got, c.code, "must be asked for again")
			if calls := env.telegram.made(); len(calls) != 0 {
				t.Errorf("calls = %q, want no ban", calls)
			}
		})
	}
}

func TestModerateFailedConfirmationSaysToAskAgain(t *testing.T) {
	env := newModerationEnv(t)
	env.moderate(asking(100, bekTarget()), `{"action":"ban"}`)
	env.telegram.err = errors.New("dial tcp: i/o timeout")

	got := env.moderate(asking(101, nil), `{"action":"confirm_ban"}`)

	expectRefusal(t, got, "execution_failed", "may not have been done; try again later; the ban no longer waits for confirmation, so it must be asked for again")
	env.telegram.err = nil
	expectRefusal(t, env.moderate(asking(102, nil), `{"action":"confirm_ban"}`), "forbidden", "no ban to confirm")
}

func TestModerateFailsClosedWhenLookupFails(t *testing.T) {
	for name, who := range map[string]int64{"the person asking": userID, "the bot": rulesBotID, "the member": memberID} {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			env.chat.failing[who] = true

			for _, action := range []string{"delete", "mute", "ban"} {
				expectRefusal(t, env.moderate(asking(100, bekTarget()), `{"action":"`+action+`"}`), "execution_failed", "nothing was done")
			}
			env.nothingDone(t)
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

	if got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`); !got.OK {
		t.Fatalf("the 10th action = %+v", got)
	}
	expectRefusal(t, env.moderate(asking(101, bekTarget()), `{"action":"mute"}`), "forbidden", "10 actions in 10 minutes")
	// An owner is held to it too.
	expectRefusal(t, env.moderate(ownerAsking(102, bekTarget()), `{"action":"delete"}`), "forbidden", "10 actions in 10 minutes")

	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want one", calls)
	}
	for _, since := range env.audit.since {
		if !since.Equal(rulesNow.Add(-10 * time.Minute)) {
			t.Errorf("counted since %v, want 10 minutes before now", since)
		}
	}
}

func TestModerateTelegramErrorIsReportedAndForgotten(t *testing.T) {
	env := newModerationEnv(t)
	const noAnswer = "Telegram did not answer, so it may not have been done; try again later"
	failures := []struct {
		err     error
		message string
	}{
		{&telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}, "Telegram refused: Bad Request: message can't be deleted"},
		// Without an answer from Telegram, or with a server's error page
		// instead, it may have acted.
		{errors.New("dial tcp: i/o timeout"), noAnswer},
		{&telegram.APIError{Code: 502, Description: "unreadable response from deleteMessage"}, noAnswer},
	}
	for _, f := range failures {
		env.telegram.err = f.err

		got := env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)

		if got.OK || got.Error.Code != "execution_failed" || got.Error.Message != f.message {
			t.Errorf("delete failing with %v = %+v, want %q", f.err, got, f.message)
		}
		if audit := env.audit.recorded(); len(audit) != 0 {
			t.Errorf("audit = %+v, want the failed action forgotten", audit)
		}
	}

	// The same update may try again once Telegram agrees.
	env.telegram.err = nil
	if got := env.moderate(asking(100, bekTarget()), `{"action":"delete"}`); !got.OK {
		t.Errorf("delete again = %+v", got)
	}
	if calls, audit := env.telegram.made(), env.audit.recorded(); len(calls) != 4 || len(audit) != 1 {
		t.Errorf("calls = %q, audit = %+v; want four tries and one record", calls, audit)
	}
}

func TestModerateReportsARecordItCannotDrop(t *testing.T) {
	env := newModerationEnv(t)
	env.telegram.err = &telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}
	env.audit.forgetErr = errors.New("storage: forget moderation: connection reset")

	got := env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)

	if got.OK || !strings.Contains(got.Error.Message, "Telegram refused: Bad Request: message can't be deleted") || !strings.Contains(got.Error.Message, "connection reset") {
		t.Errorf("delete = %+v, want Telegram's reason and the store's", got)
	}
	// The record stayed, so the update delivered again does nothing, and
	// does not claim it was done.
	env.telegram.err = nil
	got = env.moderate(asking(100, bekTarget()), `{"action":"delete"}`)
	if !got.OK || !strings.Contains(string(got.Result), `"already_handled":true`) || strings.Contains(string(got.Result), `"done"`) {
		t.Errorf("delete again = %+v %s, want already handled", got, got.Result)
	}
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
		{`{"action":"mute","minutes":20000}`, 10080, "23 Oct 15:04"},
		// Past midnight the date is given too.
		{`{"action":"mute","minutes":600}`, 600, "17 Oct 01:04"},
	}
	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			env := newModerationEnv(t)

			got := env.moderate(asking(100, bekTarget()), c.args)

			var result struct{ Until string }
			_ = json.Unmarshal(got.Result, &result)
			if !got.OK || result.Until != c.until {
				t.Errorf("mute = %+v %s, want until %q", got, got.Result, c.until)
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
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 100, Target: bekTarget()}, &Sink{})
	bot := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: false, UpdateID: 101, Target: bekTarget()}, &Sink{})

	if defs := env.registry.Definitions(private, userID); len(defs) != 0 {
		t.Errorf("offered in a private chat: %+v", defs)
	}
	expectRefusal(t, env.moderate(private, `{"action":"delete"}`), "forbidden", "group")
	expectRefusal(t, env.moderate(bot, `{"action":"delete"}`), "forbidden", "person in a group")

	// The tool checks by itself too, should it run outside the registry or
	// a turn.
	tool := ModerationTool(env.opts)
	for _, ctx := range []context.Context{private, context.Background()} {
		if _, err := tool.Run(ctx, json.RawMessage(`{"action":"delete"}`)); err == nil {
			t.Error("Run outside a group turn succeeded")
		}
	}
	env.nothingDone(t)
}

func TestModerateChecksItsArguments(t *testing.T) {
	env := newModerationEnv(t)
	for _, args := range []string{
		`{}`,
		`{"action":"kick"}`,
		`{"action":"mute","user_id":5}`,
		`{"action":"delete","message_id":12}`,
		`{"action":"mute","minutes":"ten"}`,
	} {
		if got := env.moderate(asking(100, bekTarget()), args); got.OK || got.Error.Code != "invalid_arguments" {
			t.Errorf("%s = %+v, want invalid_arguments", args, got)
		}
	}
	env.nothingDone(t)

	// A long reason is kept short in the audit.
	if got := env.moderate(asking(101, bekTarget()), `{"action":"delete","reason":"`+strings.Repeat("я", 300)+`"}`); !got.OK {
		t.Fatalf("delete with a long reason = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || len([]rune(audit[0].Reason)) > 201 {
		t.Errorf("audit = %+v, want the reason cut", audit)
	}
}
