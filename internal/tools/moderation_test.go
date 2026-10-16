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

// fakeTelegram records the moderation calls it is asked to make, and the
// questions the bot posts.
type fakeTelegram struct {
	mu           sync.Mutex
	calls        []string
	restrictions []telegram.Restriction
	questions    []telegram.Outgoing
	err          error
	// sendErr makes posting a question fail.
	sendErr error
}

func (f *fakeTelegram) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.questions = append(f.questions, msg)
	return int64(900 + len(f.questions)), nil
}

// asked returns the questions posted, and the ID of the last one.
func (f *fakeTelegram) asked() ([]telegram.Outgoing, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.Outgoing(nil), f.questions...), int64(900 + len(f.questions))
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

// fakeAudit keeps the audit and the waiting actions in memory the way the
// store does, without the times: every action counts as recent.
type fakeAudit struct {
	mu      sync.Mutex
	actions []storage.ModerationAction
	pending map[[2]int64]waiting
	// since and maxAges are what the tool asked the store for.
	since   []time.Time
	maxAges []time.Duration
	// forgetErr makes ForgetModeration fail and keep the record; countErr
	// makes ModerationBy fail.
	forgetErr, countErr error
}

// waiting is an action that waits for confirmation, and the question about
// it.
type waiting struct {
	storage.PendingModeration
	promptID int64
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

func (f *fakeAudit) UpdateModerated(_ context.Context, updateID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.actions, func(a storage.ModerationAction) bool {
		return a.UpdateID == updateID && a.Action != "poll"
	}), nil
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

func (f *fakeAudit) SavePendingModeration(_ context.Context, p storage.PendingModeration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[[2]int64]waiting{}
	}
	key := [2]int64{p.RequesterID, p.ChatID}
	if old, ok := f.pending[key]; ok && old.UpdateID == p.UpdateID {
		return false, nil
	}
	f.pending[key] = waiting{PendingModeration: p}
	return true, nil
}

func (f *fakeAudit) SetModerationPrompt(_ context.Context, requesterID, chatID, updateID, promptID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{requesterID, chatID}
	if w, ok := f.pending[key]; ok && w.UpdateID == updateID {
		w.promptID = promptID
		f.pending[key] = w
	}
	return nil
}

func (f *fakeAudit) TakePendingModeration(_ context.Context, requesterID, chatID, promptID int64, maxAge time.Duration) (storage.PendingModeration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.maxAges = append(f.maxAges, maxAge)
	key := [2]int64{requesterID, chatID}
	w, ok := f.pending[key]
	if !ok || w.promptID == 0 || w.promptID != promptID {
		return storage.PendingModeration{}, storage.ErrNoPendingModeration
	}
	delete(f.pending, key)
	return w.PendingModeration, nil
}

func (f *fakeAudit) CancelPendingModeration(_ context.Context, requesterID, chatID int64) (bool, error) {
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
		Questions:  env.telegram,
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

// nothingDone fails the test if Telegram was asked to act, or anything was
// recorded, asked or left waiting.
func (env *moderationEnv) nothingDone(t *testing.T) {
	t.Helper()
	env.nothingActed(t)
	if env.audit.waiting() != 0 {
		t.Errorf("an action is waiting for confirmation")
	}
	if questions, _ := env.telegram.asked(); len(questions) != 0 {
		t.Errorf("questions posted: %+v", questions)
	}
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

// confirm is the answer of userID, in update updateID, to the bot's question
// promptID.
func (env *moderationEnv) confirm(t *testing.T, updateID, promptID int64, yes bool) Confirmation {
	t.Helper()
	return env.confirmAs(t, Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: 60}, promptID, yes)
}

func (env *moderationEnv) confirmAs(t *testing.T, caller Caller, promptID int64, yes bool) Confirmation {
	t.Helper()
	c, err := env.moderation.Confirm(context.Background(), caller, promptID, yes)
	if err != nil {
		t.Errorf("Confirm: unexpected error %v", err)
	}
	return c
}

// ask has the tool asked for args in ctx, expects the bot's question, and
// returns its ID.
func (env *moderationEnv) ask(t *testing.T, ctx context.Context, args string) int64 {
	t.Helper()
	before := len(env.telegram.made())
	got := env.moderate(ctx, args)
	if !got.OK || !strings.Contains(string(got.Result), `"asked":true`) {
		t.Fatalf("moderate %s = %+v %s, want the question asked", args, got, got.Result)
	}
	if calls := env.telegram.made(); len(calls) != before {
		t.Fatalf("acted before the confirmation: %q", calls[before:])
	}
	_, promptID := env.telegram.asked()
	return promptID
}

// done asks for args by userID in update 100 and confirms it in update 101.
func (env *moderationEnv) done(t *testing.T, ctx context.Context, args string) Confirmation {
	t.Helper()
	return env.confirm(t, 101, env.ask(t, ctx, args), true)
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

func TestModerateDoesNothingUntilTheRequesterConfirms(t *testing.T) {
	env := newModerationEnv(t)

	got := env.moderate(asking(100, bekTarget()), `{"action":"mute","minutes":10080,"reason":"spam"}`)

	want := `{"action":"mute","asked":true,"note":"Nothing is done yet. I posted my own question asking them to confirm by replying yes to it; their answer is handled without you. Don't ask again and don't say it is done: reply with exactly [react 👌].","target":"Bek @bek"}`
	if !got.OK || string(got.Result) != want {
		t.Fatalf("mute = %+v %s\nwant %s", got, got.Result, want)
	}
	env.nothingActed(t)
	// The question is the bot's own words, a reply to the request, and it
	// says exactly what will be done; the model's reason is not in it.
	questions, promptID := env.telegram.asked()
	wantQuestion := telegram.Outgoing{ChatID: groupA, ReplyToMessageID: 50,
		Text: "Confirm: mute Bek @bek for 7 days? Reply yes (ha, да) to this message within 5 minutes, or no to cancel."}
	if len(questions) != 1 || questions[0] != wantQuestion {
		t.Fatalf("questions = %+v\nwant [%+v]", questions, wantQuestion)
	}

	c := env.confirm(t, 101, promptID, true)

	if want := (Confirmation{Found: true, Text: "Done: Bek @bek is muted until 23 Oct 15:04."}); c != want {
		t.Errorf("confirmation = %+v, want %+v", c, want)
	}
	restrictions := env.telegram.restricted()
	if len(restrictions) != 1 {
		t.Fatalf("restrictions = %+v, want one", restrictions)
	}
	if r := restrictions[0]; r.ChatID != groupA || r.UserID != memberID || r.Permissions != telegram.NoPermissions() || !r.Until.Equal(rulesNow.Add(7*24*time.Hour)) {
		t.Errorf("restriction = %+v, want Bek muted for a week", r)
	}
	wantAudit := storage.ModerationAction{ChatID: groupA, Action: "mute", RequesterID: userID, TargetUserID: memberID, TargetMessageID: 29, Minutes: 10080, Reason: "spam", UpdateID: 101}
	if got := env.audit.recorded(); len(got) != 1 || got[0] != wantAudit {
		t.Errorf("audit = %+v\nwant    [%+v]", got, wantAudit)
	}
	if saved, want := env.rights.all(), fmt.Sprintf("%d administrator %v", groupA, moderationRights); len(saved) != 2 || saved[1] != want {
		t.Errorf("stored rights = %q, want %q after each check", saved, want)
	}
	for _, maxAge := range env.audit.maxAges {
		if maxAge != 5*time.Minute {
			t.Errorf("an action may wait %v, want 5 minutes", maxAge)
		}
	}
}

func TestModerateActions(t *testing.T) {
	cases := []struct {
		action, call, question, done string
		// text is what the audit keeps of the message.
		text string
	}{
		{"delete", "deleteMessage -100111 29", "delete the message by Bek @bek", "Done: the message by Bek @bek is deleted.", "you're slow"},
		{"unmute", "restrictChatMember -100111 5005", "unmute Bek @bek", "Done: Bek @bek can write again.", ""},
		{"ban", "banChatMember -100111 5005", "ban Bek @bek", "Done: Bek @bek is banned.", ""},
		{"unban", "unbanChatMember -100111 5005", "unban Bek @bek", "Done: Bek @bek is unbanned and may join again.", ""},
		{"pin", "pinChatMessage -100111 29", "pin the message by Bek @bek", "Done: the message by Bek @bek is pinned.", ""},
		{"unpin", "unpinChatMessage -100111 29", "unpin the message by Bek @bek", "Done: the message by Bek @bek is unpinned.", ""},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			env := newModerationEnv(t)

			got := env.done(t, asking(100, bekTarget()), `{"action":"`+c.action+`"}`)

			if questions, _ := env.telegram.asked(); !strings.HasPrefix(questions[0].Text, "Confirm: "+c.question+"? ") {
				t.Errorf("question = %q, want it to ask to %s", questions[0].Text, c.question)
			}
			if got != (Confirmation{Found: true, Text: c.done}) {
				t.Errorf("confirmation = %+v, want %q", got, c.done)
			}
			if calls := env.telegram.made(); len(calls) != 1 || calls[0] != c.call {
				t.Errorf("calls = %q, want [%q]", calls, c.call)
			}
			if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != c.action || audit[0].TargetText != c.text || audit[0].Minutes != 0 || audit[0].UpdateID != 101 {
				t.Errorf("audit = %+v, want %s with text %q in the confirming update", audit, c.action, c.text)
			}
		})
	}

	// Unmuting lifts every restriction, for good.
	env := newModerationEnv(t)
	env.done(t, asking(100, bekTarget()), `{"action":"unmute"}`)
	if r := env.telegram.restricted(); len(r) != 1 || r[0].Permissions != telegram.AllPermissions() || !r[0].Until.IsZero() {
		t.Errorf("unmute restriction = %+v, want every permission back", r)
	}
}

func TestModerateConfirmationIsTheRequestersReplyToTheQuestion(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)

	// Another admin's yes, or a yes to another message, is no answer: the
	// message is then an ordinary one.
	const otherAdmin = int64(9004)
	env.chat.set(otherAdmin, admin(moderationRights...))
	other := Caller{UserID: otherAdmin, ChatID: groupA, Human: true, UpdateID: 101}
	if c := env.confirmAs(t, other, promptID, true); c.Found {
		t.Errorf("another admin's yes = %+v", c)
	}
	if c := env.confirm(t, 102, promptID-1, true); c.Found {
		t.Errorf("a yes to another message = %+v", c)
	}
	env.nothingActed(t)

	if c := env.confirm(t, 103, promptID, true); c.Text != "Done: Bek @bek is banned." {
		t.Fatalf("the requester's yes = %+v", c)
	}
	// It is used up: the same question confirms nothing more.
	if c := env.confirm(t, 104, promptID, true); c.Found {
		t.Errorf("a second yes = %+v", c)
	}
	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want the one ban", calls)
	}
}

func TestModerateNoCancels(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)

	if c := env.confirm(t, 101, promptID, false); c != (Confirmation{Found: true, Text: "Cancelled: nothing was done."}) {
		t.Errorf("no = %+v", c)
	}
	if c := env.confirm(t, 102, promptID, true); c.Found {
		t.Errorf("a yes after the no = %+v", c)
	}
	env.nothingActed(t)
}

func TestModerateCancelByTheModel(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)

	if got := env.moderate(asking(101, nil), `{"action":"cancel"}`); !got.OK || string(got.Result) != `{"cancelled":true}` {
		t.Errorf("cancel = %+v %s", got, got.Result)
	}
	if c := env.confirm(t, 102, promptID, true); c.Found {
		t.Errorf("a yes after the cancel = %+v", c)
	}
	if got := env.moderate(asking(103, nil), `{"action":"cancel"}`); !got.OK || string(got.Result) != `{"cancelled":false}` {
		t.Errorf("cancel again = %+v %s", got, got.Result)
	}
	env.nothingActed(t)
}

func TestModerateLaterRequestReplacesTheQuestion(t *testing.T) {
	env := newModerationEnv(t)
	first := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)
	second := env.ask(t, asking(101, bekTarget()), `{"action":"mute"}`)

	if c := env.confirm(t, 102, first, true); c.Found {
		t.Errorf("a yes to the replaced question = %+v", c)
	}
	if c := env.confirm(t, 103, second, true); c.Text != "Done: Bek @bek is muted until 16:04." {
		t.Errorf("a yes to the latest question = %+v", c)
	}
	if calls := env.telegram.made(); len(calls) != 1 || !strings.HasPrefix(calls[0], "restrictChatMember") {
		t.Errorf("calls = %q, want only the mute", calls)
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
	promptID := env.ask(t, ownerAsking(100, bekTarget()), `{"action":"delete"}`)

	c := env.confirmAs(t, Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 101}, promptID, true)

	if !strings.HasPrefix(c.Text, "Done:") {
		t.Fatalf("delete by an owner = %+v", c)
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

	expectRefusal(t, env.moderate(ownerAsking(100, bekTarget()), `{"action":"delete"}`), "forbidden", "only this group's admins with that right")
	env.nothingDone(t)

	// An admin may have an owner's message deleted.
	env.chat.set(userID, admin(moderationRights...))
	owners := &Target{MessageID: 29, UserID: 9002, Name: "Muhammad", Owner: true}
	if c := env.done(t, asking(101, owners), `{"action":"delete"}`); !strings.HasPrefix(c.Text, "Done:") {
		t.Errorf("deleting an owner's message = %+v", c)
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

	if c := env.done(t, asking(100, botMessage()), `{"action":"delete"}`); !strings.HasPrefix(c.Text, "Done:") {
		t.Fatalf("deleting the bot's own message = %+v", c)
	}
	expectRefusal(t, env.moderate(asking(102, botMessage()), `{"action":"mute"}`), "forbidden", "I can't do that to")

	// The person asking may have their own message deleted or pinned, but
	// may not mute or ban themselves.
	own := &Target{MessageID: 27, UserID: userID, Name: "Ali"}
	for i, action := range []string{"delete", "pin", "unpin"} {
		promptID := env.ask(t, asking(int64(110+2*i), own), `{"action":"`+action+`"}`)
		if c := env.confirm(t, int64(111+2*i), promptID, true); !strings.HasPrefix(c.Text, "Done:") {
			t.Errorf("%s of one's own message = %+v", action, c)
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
	env.nothingDone(t)
}

func TestModerateSameUpdateTwiceAsksOnce(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"mute"}`)

	got := env.moderate(asking(100, bekTarget()), `{"action":"mute"}`)

	want := `{"already_asked":true,"note":"This message was delivered twice and the first delivery already asked them to confirm, so nothing more was done now: reply with exactly [silent]."}`
	if !got.OK || string(got.Result) != want {
		t.Errorf("mute again = %+v %s\nwant %s", got, got.Result, want)
	}
	if questions, _ := env.telegram.asked(); len(questions) != 1 {
		t.Errorf("questions = %+v, want one", questions)
	}
	// The first question still stands.
	if c := env.confirm(t, 101, promptID, true); !strings.HasPrefix(c.Text, "Done:") {
		t.Errorf("confirmation = %+v", c)
	}
}

func TestModerateSameConfirmationTwiceActsOnce(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"mute"}`)
	env.confirm(t, 101, promptID, true)

	// Telegram delivered the yes again: it is known, and nothing is said.
	if c := env.confirm(t, 101, promptID, true); c != (Confirmation{Found: true}) {
		t.Errorf("the same yes again = %+v, want found and silent", c)
	}
	if calls, audit := env.telegram.made(), env.audit.recorded(); len(calls) != 1 || len(audit) != 1 {
		t.Errorf("calls = %q, audit = %+v; want one of each", calls, audit)
	}
}

func TestModerateConfirmationRechecksRights(t *testing.T) {
	cases := map[string]struct {
		change func(env *moderationEnv)
		text   string
	}{
		"the person asking lost the right": {func(env *moderationEnv) { env.chat.set(userID, admin(telegram.RightDeleteMessages)) }, "Not done: only this group's admins with that right, or my owners, can ask for that."},
		"the bot lost the right":           {func(env *moderationEnv) { env.chat.set(rulesBotID, admin(telegram.RightPinMessages)) }, "Not done: I don't have the right to ban and mute members here."},
		"the member became an admin":       {func(env *moderationEnv) { env.chat.set(memberID, admin()) }, "Not done: I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking."},
		"Telegram cannot say":              {func(env *moderationEnv) { env.chat.failing[memberID] = true }, "Not done: could not ask Telegram who may do this, so nothing was done; try again later."},
		"the person asking did 10 actions": {func(env *moderationEnv) {
			for i := range 10 {
				_, _ = env.audit.RecordModeration(context.Background(), storage.ModerationAction{ChatID: groupA, Action: "delete", RequesterID: userID, UpdateID: int64(i + 1)})
			}
		}, "Not done: that's 10 actions in 10 minutes; wait a bit."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newModerationEnv(t)
			promptID := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)
			c.change(env)

			got := env.confirm(t, 101, promptID, true)

			if got != (Confirmation{Found: true, Text: c.text}) {
				t.Errorf("confirmation = %+v, want %q", got, c.text)
			}
			if calls := env.telegram.made(); len(calls) != 0 {
				t.Errorf("calls = %q, want no ban", calls)
			}
		})
	}
}

func TestModerateConfirmationTellsNothingOfTheStore(t *testing.T) {
	env := newModerationEnv(t)
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"ban"}`)
	env.audit.countErr = errors.New("storage: count moderation: failed to connect to `user=maria_app database=maria_prod`")

	c, err := env.moderation.Confirm(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 101}, promptID, true)

	if c != (Confirmation{Found: true, Text: "Not done: something went wrong on my side; ask again later."}) {
		t.Errorf("confirmation = %+v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "maria_prod") {
		t.Errorf("err = %v, want the cause for the logs", err)
	}
	env.nothingActed(t)
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

	if c := env.done(t, asking(100, bekTarget()), `{"action":"mute"}`); !strings.HasPrefix(c.Text, "Done:") {
		t.Fatalf("the 10th action = %+v", c)
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

func TestModerateQuestionThatCannotBePostedLeavesNothingWaiting(t *testing.T) {
	env := newModerationEnv(t)
	env.telegram.sendErr = errors.New("dial tcp: i/o timeout")

	got := env.moderate(asking(100, bekTarget()), `{"action":"ban"}`)

	expectRefusal(t, got, "execution_failed", "I could not post the question that asks them to confirm, so nothing will be done")
	env.nothingDone(t)
}

func TestModerateTelegramErrorIsReported(t *testing.T) {
	const noAnswer = "Not done: Telegram did not answer, so it may not have been done; try again later."
	failures := []struct {
		err  error
		text string
		// kept is whether the audit keeps the action: Telegram may have
		// taken it unless it refused.
		kept bool
	}{
		{&telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}, "Not done: Telegram refused: Bad Request: message can't be deleted.", false},
		// Without an answer from Telegram, or with a server's error page
		// instead, it may have acted.
		{errors.New("dial tcp: i/o timeout"), noAnswer, true},
		{&telegram.APIError{Code: 502, Description: "unreadable response from deleteMessage"}, noAnswer, true},
	}
	for _, f := range failures {
		env := newModerationEnv(t)
		env.telegram.err = f.err

		promptID := env.ask(t, asking(100, bekTarget()), `{"action":"delete"}`)
		got := env.confirm(t, 101, promptID, true)

		if got != (Confirmation{Found: true, Text: f.text}) {
			t.Errorf("delete failing with %v = %+v, want %q", f.err, got, f.text)
		}
		audit := env.audit.recorded()
		if kept := len(audit) == 1 && audit[0].TargetText == "you're slow"; kept != f.kept {
			t.Errorf("delete failing with %v: audit = %+v, want kept %t", f.err, audit, f.kept)
		}
		// The yes delivered again acts no more when the action may be done.
		env.telegram.err = nil
		again := env.confirm(t, 101, promptID, true)
		if f.kept && again != (Confirmation{Found: true}) {
			t.Errorf("the yes again after %v = %+v, want found and silent", f.err, again)
		}
		if calls := env.telegram.made(); len(calls) != 1 {
			t.Errorf("calls after %v = %q, want one", f.err, calls)
		}
	}
}

func TestModerateReportsARecordItCannotDrop(t *testing.T) {
	env := newModerationEnv(t)
	env.telegram.err = &telegram.APIError{Code: 400, Description: "Bad Request: message can't be deleted"}
	env.audit.forgetErr = errors.New("storage: forget moderation: connection reset")
	promptID := env.ask(t, asking(100, bekTarget()), `{"action":"delete"}`)

	c, err := env.moderation.Confirm(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 101}, promptID, true)

	// The chat is told Telegram's reason only; the logs get the store's too.
	if c != (Confirmation{Found: true, Text: "Not done: Telegram refused: Bad Request: message can't be deleted."}) {
		t.Errorf("confirmation = %+v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the store's failure", err)
	}
	// The record stayed, so the yes delivered again does nothing more.
	env.telegram.err = nil
	if c := env.confirm(t, 101, promptID, true); c != (Confirmation{Found: true}) {
		t.Errorf("the yes again = %+v, want found and silent", c)
	}
	if calls := env.telegram.made(); len(calls) != 1 {
		t.Errorf("calls = %q, want the one refused", calls)
	}
}

func TestModerateMinutesClamped(t *testing.T) {
	cases := []struct {
		args     string
		minutes  int
		duration string
		until    string
	}{
		{`{"action":"mute"}`, 60, "1 hour", "16:04"},
		{`{"action":"mute","minutes":0}`, 60, "1 hour", "16:04"},
		{`{"action":"mute","minutes":90}`, 90, "90 minutes", "16:34"},
		{`{"action":"mute","minutes":-5}`, 1, "1 minute", "15:05"},
		{`{"action":"mute","minutes":20000}`, 10080, "7 days", "23 Oct 15:04"},
		// Past midnight the date is given too.
		{`{"action":"mute","minutes":600}`, 600, "10 hours", "17 Oct 01:04"},
	}
	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			env := newModerationEnv(t)

			got := env.done(t, asking(100, bekTarget()), c.args)

			if questions, _ := env.telegram.asked(); !strings.Contains(questions[0].Text, "mute Bek @bek for "+c.duration+"?") {
				t.Errorf("question = %q, want %s", questions[0].Text, c.duration)
			}
			if want := "Done: Bek @bek is muted until " + c.until + "."; got.Text != want {
				t.Errorf("confirmation = %+v, want %q", got, want)
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
	tool := env.moderation.Tool()
	for _, ctx := range []context.Context{private, context.Background()} {
		if _, err := tool.Run(ctx, json.RawMessage(`{"action":"delete"}`)); err == nil {
			t.Error("Run outside a group turn succeeded")
		}
	}
	// Neither can a bot or a private chat confirm anything.
	for _, caller := range []Caller{
		{UserID: userID, ChatID: groupA, Human: false, UpdateID: 102},
		{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 103},
	} {
		if c := env.confirmAs(t, caller, 901, true); c.Found {
			t.Errorf("Confirm by %+v = %+v", caller, c)
		}
	}
	env.nothingDone(t)
}

func TestModerateChecksItsArguments(t *testing.T) {
	env := newModerationEnv(t)
	for _, args := range []string{
		`{}`,
		`{"action":"kick"}`,
		// The model cannot confirm: only the person's own reply does.
		`{"action":"confirm_ban"}`,
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
	if c := env.done(t, asking(101, bekTarget()), `{"action":"delete","reason":"`+strings.Repeat("я", 300)+`"}`); !strings.HasPrefix(c.Text, "Done:") {
		t.Fatalf("delete with a long reason = %+v", c)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || len([]rune(audit[0].Reason)) > 201 {
		t.Errorf("audit = %+v, want the reason cut", audit)
	}
}

func TestConfirmationAnswer(t *testing.T) {
	cases := map[string]struct{ yes, ok bool }{
		"ha": {true, true}, "Ha!": {true, true}, " xa ": {true, true}, "ҳа": {true, true}, "Да": {true, true},
		"yes": {true, true}, "OK.": {true, true}, "+": {true, true}, "👍": {true, true}, "👍🏻": {true, true},
		"albatta": {true, true}, "ha)": {true, true},
		"yo'q": {false, true}, "Нет": {false, true}, "no": {false, true}, "-": {false, true}, "👎": {false, true},
		// Anything more than the answer is an ordinary message.
		"ha, lekin uni emas": {false, false}, "yes ban him": {false, false}, "haha": {false, false},
		"": {false, false}, "😂": {false, false}, "ha?": {false, false},
	}
	for text, want := range cases {
		if yes, ok := ConfirmationAnswer(text); yes != want.yes || ok != want.ok {
			t.Errorf("ConfirmationAnswer(%q) = %t, %t; want %t, %t", text, yes, ok, want.yes, want.ok)
		}
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
	env.nothingDone(t)
}
