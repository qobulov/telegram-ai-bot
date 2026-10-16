package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakePollSender records the polls it is asked to post.
type fakePollSender struct {
	mu    sync.Mutex
	polls []telegram.OutgoingPoll
	err   error
}

func (f *fakePollSender) SendPoll(_ context.Context, poll telegram.OutgoingPoll) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls = append(f.polls, poll)
	if f.err != nil {
		return 0, f.err
	}
	return int64(900 + len(f.polls)), nil
}

func (f *fakePollSender) sent() []telegram.OutgoingPoll {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.OutgoingPoll(nil), f.polls...)
}

// pollRecord is one audit row and when it was written.
type pollRecord struct {
	action storage.ModerationAction
	at     time.Time
}

// fakePollAudit keeps the audit in memory the way the store does, stamping
// each row with the test's clock.
type fakePollAudit struct {
	mu      sync.Mutex
	now     func() time.Time
	records []pollRecord
	since   []time.Time
}

func (f *fakePollAudit) RecordModeration(_ context.Context, a storage.ModerationAction) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.ContainsFunc(f.records, func(r pollRecord) bool {
		return r.action.UpdateID == a.UpdateID && r.action.Action == a.Action
	}) {
		return false, nil
	}
	f.records = append(f.records, pollRecord{action: a, at: f.now()})
	return true, nil
}

func (f *fakePollAudit) ForgetModeration(_ context.Context, updateID int64, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = slices.DeleteFunc(f.records, func(r pollRecord) bool {
		return r.action.UpdateID == updateID && r.action.Action == action
	})
	return nil
}

func (f *fakePollAudit) ChatModeration(_ context.Context, chatID int64, action string, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = append(f.since, since)
	count := 0
	for _, r := range f.records {
		if r.action.ChatID == chatID && r.action.Action == action && !r.at.Before(since) {
			count++
		}
	}
	return count, nil
}

func (f *fakePollAudit) recorded() []storage.ModerationAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	actions := make([]storage.ModerationAction, 0, len(f.records))
	for _, r := range f.records {
		actions = append(actions, r.action)
	}
	return actions
}

type pollEnv struct {
	sender   *fakePollSender
	audit    *fakePollAudit
	now      time.Time
	opts     PollOptions
	registry *Registry
}

func newPollEnv(t *testing.T) *pollEnv {
	t.Helper()
	env := &pollEnv{sender: &fakePollSender{}, now: rulesNow}
	env.audit = &fakePollAudit{now: env.clock}
	env.opts = PollOptions{Sender: env.sender, Store: env.audit, Now: env.clock}
	env.registry = mustRegistry(t, PollTool(env.opts))
	return env
}

func (env *pollEnv) clock() time.Time { return env.now }

func (env *pollEnv) poll(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "create_poll", args)), &result)
	return result
}

// memberAsking is a plain member's turn in chatID, in update updateID.
func memberAsking(chatID, updateID int64) context.Context {
	return WithTurn(context.Background(), Caller{UserID: memberID, ChatID: chatID, Human: true, UpdateID: updateID, MessageID: 60}, &Sink{})
}

const lunchPoll = `{"question":"Lunch?","options":["Plov","Lagman"]}`

func TestPollSent(t *testing.T) {
	env := newPollEnv(t)

	got := env.poll(memberAsking(groupA, 100), `{"question":"  Lunch where?  ","options":["Plov"," Lagman ","Somsa"],"multiple":true}`)

	if !got.OK || !strings.Contains(string(got.Result), `"sent":true`) {
		t.Fatalf("create_poll = %+v %s, want sent", got, got.Result)
	}
	want := telegram.OutgoingPoll{ChatID: groupA, Question: "Lunch where?", Options: []string{"Plov", "Lagman", "Somsa"}, Multiple: true}
	polls := env.sender.sent()
	if len(polls) != 1 || polls[0].ChatID != want.ChatID || polls[0].Question != want.Question ||
		!slices.Equal(polls[0].Options, want.Options) || polls[0].Multiple != want.Multiple {
		t.Errorf("polls = %+v\nwant    [%+v]", polls, want)
	}
	wantAudit := storage.ModerationAction{ChatID: groupA, Action: "poll", RequesterID: memberID, UpdateID: 100}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0] != wantAudit {
		t.Errorf("audit = %+v\nwant    [%+v]", audit, wantAudit)
	}
	for _, since := range env.audit.since {
		if !since.Equal(rulesNow.Add(-10 * time.Minute)) {
			t.Errorf("counted polls since %v, want 10 minutes before now", since)
		}
	}
}

func TestPollAtTheLimitsIsSent(t *testing.T) {
	env := newPollEnv(t)
	options := make([]string, 10)
	for i := range options {
		options[i] = strings.Repeat("я", 99) + string(rune('a'+i))
	}
	args, _ := json.Marshal(map[string]any{"question": strings.Repeat("я", 300), "options": options})

	if got := env.poll(memberAsking(groupA, 100), string(args)); !got.OK {
		t.Fatalf("create_poll = %+v, want the longest poll allowed sent", got)
	}
	if polls := env.sender.sent(); len(polls) != 1 || polls[0].Multiple {
		t.Errorf("polls = %+v, want one single-answer poll", polls)
	}
}

func TestPollValidation(t *testing.T) {
	cases := map[string]string{
		"one option":             `{"question":"Lunch?","options":["Plov"]}`,
		"no options":             `{"question":"Lunch?","options":[]}`,
		"eleven options":         `{"question":"Lunch?","options":["1","2","3","4","5","6","7","8","9","10","11"]}`,
		"duplicate options":      `{"question":"Lunch?","options":["Plov"," plov "]}`,
		"an empty option":        `{"question":"Lunch?","options":["Plov","  "]}`,
		"a 101-character option": `{"question":"Lunch?","options":["Plov","` + strings.Repeat("я", 101) + `"]}`,
		"an empty question":      `{"question":"   ","options":["Plov","Lagman"]}`,
		"no question":            `{"options":["Plov","Lagman"]}`,
		"a 301-rune question":    `{"question":"` + strings.Repeat("я", 301) + `","options":["Plov","Lagman"]}`,
		"an unknown argument":    `{"question":"Lunch?","options":["Plov","Lagman"],"chat_id":-100222}`,
		"multiple as text":       `{"question":"Lunch?","options":["Plov","Lagman"],"multiple":"yes"}`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			env := newPollEnv(t)

			got := env.poll(memberAsking(groupA, 100), args)

			if got.OK || got.Error.Code != "invalid_arguments" {
				t.Errorf("create_poll = %+v, want invalid_arguments", got)
			}
			if polls, audit := env.sender.sent(), env.audit.recorded(); len(polls) != 0 || len(audit) != 0 {
				t.Errorf("polls = %+v, audit = %+v; want nothing sent or recorded", polls, audit)
			}
		})
	}
}

func TestPollOncePerTenMinutes(t *testing.T) {
	env := newPollEnv(t)
	if got := env.poll(memberAsking(groupA, 100), lunchPoll); !got.OK {
		t.Fatalf("first poll = %+v", got)
	}

	env.now = rulesNow.Add(9 * time.Minute)
	got := env.poll(memberAsking(groupA, 101), lunchPoll)

	if got.OK || got.Error.Code != "invalid_arguments" || got.Error.Message != "a poll was made here less than 10 minutes ago" {
		t.Errorf("second poll = %+v, want refused", got)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0].UpdateID != 100 {
		t.Errorf("audit = %+v, want only the first poll", audit)
	}
	// Another group has its own limit.
	if got := env.poll(memberAsking(groupB, 102), lunchPoll); !got.OK {
		t.Errorf("poll in another group = %+v", got)
	}
	// The refused message delivered again is refused again.
	if got := env.poll(memberAsking(groupA, 101), lunchPoll); got.OK {
		t.Errorf("refused poll delivered again = %+v %s, want refused", got, got.Result)
	}
	// Ten minutes after the first, the group may have another.
	env.now = rulesNow.Add(10*time.Minute + time.Second)
	if got := env.poll(memberAsking(groupA, 103), lunchPoll); !got.OK {
		t.Errorf("poll after 10 minutes = %+v", got)
	}

	var chats []int64
	for _, poll := range env.sender.sent() {
		chats = append(chats, poll.ChatID)
	}
	if want := []int64{groupA, groupB, groupA}; !slices.Equal(chats, want) {
		t.Errorf("polls sent in %v, want %v", chats, want)
	}
}

func TestPollSameUpdateTwiceSendsOnce(t *testing.T) {
	env := newPollEnv(t)
	if got := env.poll(memberAsking(groupA, 100), lunchPoll); !got.OK {
		t.Fatalf("first delivery = %+v", got)
	}

	got := env.poll(memberAsking(groupA, 100), lunchPoll)

	// The first delivery may still be waiting for Telegram, or fail yet, so
	// the second says nothing of how it went.
	want := `{"action":"poll","already_handled":true,"note":"This message was delivered twice and the first delivery already asked for this, so nothing more was done now. Don't say whether it worked."}`
	if !got.OK || string(got.Result) != want {
		t.Errorf("second delivery = %+v %s\nwant %s", got, got.Result, want)
	}
	if polls := env.sender.sent(); len(polls) != 1 {
		t.Errorf("polls = %+v, want one", polls)
	}
	if audit := env.audit.recorded(); len(audit) != 1 {
		t.Errorf("audit = %+v, want one", audit)
	}
}

func TestPollSendFailureIsReportedAndForgotten(t *testing.T) {
	env := newPollEnv(t)
	env.sender.err = &telegram.APIError{Code: 400, Description: "Bad Request: not enough rights to send polls to the chat"}

	got := env.poll(memberAsking(groupA, 100), lunchPoll)

	if got.OK || got.Error.Code != "execution_failed" || got.Error.Message != "Telegram refused: Bad Request: not enough rights to send polls to the chat" {
		t.Errorf("create_poll = %+v, want Telegram's reason", got)
	}
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("audit = %+v, want the failed poll forgotten", audit)
	}

	env.sender.err = errors.New("dial tcp: i/o timeout")
	if got := env.poll(memberAsking(groupA, 100), lunchPoll); got.OK || !strings.Contains(got.Error.Message, "may not have been done") {
		t.Errorf("create_poll without an answer = %+v", got)
	}

	// The failed poll does not count, and the same update may try again.
	env.sender.err = nil
	if got := env.poll(memberAsking(groupA, 100), lunchPoll); !got.OK {
		t.Errorf("create_poll again = %+v", got)
	}
	if polls, audit := env.sender.sent(), env.audit.recorded(); len(polls) != 3 || len(audit) != 1 {
		t.Errorf("polls = %d, audit = %+v; want three tries and one record", len(polls), audit)
	}
}

func TestPollHiddenInPrivate(t *testing.T) {
	env := newPollEnv(t)
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 100}, &Sink{})

	if defs := env.registry.Definitions(private, userID); len(defs) != 0 {
		t.Errorf("offered in a private chat: %+v", defs)
	}
	if got := env.poll(private, lunchPoll); got.OK || got.Error.Code != "forbidden" {
		t.Errorf("create_poll in private = %+v, want forbidden", got)
	}
	// Any member of a group is offered it.
	if defs := env.registry.Definitions(memberAsking(groupA, 101), memberID); len(defs) != 1 || defs[0].Function.Name != "create_poll" {
		t.Errorf("definitions for a member = %+v, want create_poll", defs)
	}

	// The tool checks by itself too, should it run outside the registry or
	// a turn.
	tool := PollTool(env.opts)
	if !json.Valid(tool.Schema) {
		t.Errorf("schema is not JSON: %s", tool.Schema)
	}
	for _, ctx := range []context.Context{private, context.Background()} {
		if _, err := tool.Run(ctx, json.RawMessage(lunchPoll)); err == nil {
			t.Error("Run outside a group turn succeeded")
		}
	}
	if polls, audit := env.sender.sent(), env.audit.recorded(); len(polls) != 0 || len(audit) != 0 {
		t.Errorf("polls = %+v, audit = %+v; want nothing", polls, audit)
	}
}
