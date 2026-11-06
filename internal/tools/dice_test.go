package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

type diceRoll struct {
	chatID, replyTo int64
	emoji           string
}

// fakeDiceSender rolls value every time, as Telegram would draw it.
type fakeDiceSender struct {
	mu    sync.Mutex
	rolls []diceRoll
	value int
	err   error
}

func (f *fakeDiceSender) SendDice(_ context.Context, chatID int64, emoji string, replyTo int64) (int64, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolls = append(f.rolls, diceRoll{chatID: chatID, replyTo: replyTo, emoji: emoji})
	if f.err != nil {
		return 0, 0, f.err
	}
	return int64(800 + len(f.rolls)), f.value, nil
}

func (f *fakeDiceSender) sent() []diceRoll {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]diceRoll(nil), f.rolls...)
}

type diceEnv struct {
	sender   *fakeDiceSender
	audit    *fakePollAudit
	now      time.Time
	registry *Registry
}

func newDiceEnv(t *testing.T) *diceEnv {
	t.Helper()
	env := &diceEnv{sender: &fakeDiceSender{value: 6}, now: rulesNow}
	env.audit = &fakePollAudit{now: func() time.Time { return env.now }}
	env.registry = mustRegistry(t, DiceTool(DiceOptions{Sender: env.sender, Store: env.audit, Now: func() time.Time { return env.now }}))
	return env
}

func (env *diceEnv) roll(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "send_dice", args)), &result)
	return result
}

// A roll replies to the request, in the chat the turn is in, and gives the
// model Telegram's value and what it means.
func TestDiceRolledAndReported(t *testing.T) {
	env := newDiceEnv(t)

	got := env.roll(memberAsking(groupA, 100), `{"emoji":"🎯"}`)

	if !got.OK {
		t.Fatalf("send_dice = %+v", got)
	}
	var result map[string]any
	_ = json.Unmarshal(got.Result, &result)
	if result["value"] != float64(6) || result["max"] != float64(6) || result["outcome"] != "bullseye" {
		t.Errorf("result = %s", got.Result)
	}
	if rolls := env.sender.sent(); len(rolls) != 1 || rolls[0] != (diceRoll{chatID: groupA, replyTo: 60, emoji: "🎯"}) {
		t.Errorf("rolls = %+v", rolls)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != "dice" || audit[0].RequesterID != memberID || audit[0].UpdateID != 100 {
		t.Errorf("audit = %+v", audit)
	}
}

// Private chats roll too, a die when no game is named.
func TestDiceInAPrivateChatDefaultsToTheDie(t *testing.T) {
	env := newDiceEnv(t)

	if got := env.roll(memberAsking(memberID, 100), `{}`); !got.OK || !strings.Contains(string(got.Result), "rolled 6") {
		t.Fatalf("send_dice = %+v %s", got, got.Result)
	}
	if rolls := env.sender.sent(); len(rolls) != 1 || rolls[0].emoji != "🎲" || rolls[0].chatID != memberID {
		t.Errorf("rolls = %+v", rolls)
	}
}

func TestDiceRefusesAnotherEmoji(t *testing.T) {
	env := newDiceEnv(t)

	if got := env.roll(memberAsking(groupA, 100), `{"emoji":"🃏"}`); got.OK || got.Error.Code != codeInvalidArguments {
		t.Errorf("send_dice = %+v", got)
	}
	if len(env.sender.sent()) != 0 || len(env.audit.recorded()) != 0 {
		t.Error("an unknown emoji was rolled or recorded")
	}
}

// One message rolls once, and a chat gets diceLimit rolls in diceWindow;
// a refused roll does not count, and the window frees up again.
func TestDiceArePaced(t *testing.T) {
	env := newDiceEnv(t)
	ctx := memberAsking(groupA, 100)
	env.roll(ctx, `{}`)
	if got := env.roll(ctx, `{}`); !got.OK || !strings.Contains(string(got.Result), "already_handled") {
		t.Errorf("second roll in one message = %+v %s", got, got.Result)
	}
	for update := int64(101); update < 100+diceLimit; update++ {
		if got := env.roll(memberAsking(groupA, update), `{}`); !got.OK {
			t.Fatalf("roll %d = %+v", update, got)
		}
	}
	got := env.roll(memberAsking(groupA, 200), `{}`)
	if got.OK || !strings.Contains(got.Error.Message, "3 rolls in the last 5 minutes") {
		t.Errorf("roll over the limit = %+v", got)
	}
	if rolls := len(env.sender.sent()); rolls != diceLimit {
		t.Errorf("%d rolls, want %d", rolls, diceLimit)
	}
	if got := env.roll(memberAsking(groupB, 201), `{}`); !got.OK {
		t.Errorf("another chat's roll = %+v", got)
	}
	env.now = env.now.Add(diceWindow + time.Second)
	if got := env.roll(memberAsking(groupA, 202), `{}`); !got.OK {
		t.Errorf("roll after the window = %+v", got)
	}
}

// A roll Telegram refused is forgotten, so it may be asked for again; one
// it did not answer is kept, since it may be in the chat.
func TestDiceTelegramFailures(t *testing.T) {
	env := newDiceEnv(t)
	env.sender.err = &telegram.APIError{Code: 400, Description: "Bad Request: not enough rights to send dice"}
	if got := env.roll(memberAsking(groupA, 100), `{}`); got.OK || !strings.Contains(got.Error.Message, "not enough rights") {
		t.Errorf("refused roll = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("a refused roll is still recorded: %+v", audit)
	}
	env.sender.err = errors.New("connection reset")
	if got := env.roll(memberAsking(groupA, 101), `{}`); got.OK || !strings.Contains(got.Error.Message, "may be in the chat") {
		t.Errorf("unanswered roll = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 1 {
		t.Errorf("audit = %+v, want the unanswered roll kept", audit)
	}
}

func TestDiceOutcomes(t *testing.T) {
	cases := []struct {
		emoji string
		value int
		want  string
	}{
		{"🎲", 4, "rolled 4"},
		{"🎯", 1, "missed the board"},
		{"🎯", 3, "hit the board, not the centre"},
		{"🏀", 3, "missed"},
		{"🏀", 4, "scored"},
		{"⚽", 2, "missed"},
		{"⚽", 3, "goal"},
		{"🎳", 6, "strike"},
		{"🎳", 1, "missed every pin"},
		{"🎰", 1, "bar bar bar: three of a kind, a win"},
		{"🎰", 22, "grapes grapes grapes: three of a kind, a win"},
		{"🎰", 43, "lemon lemon lemon: three of a kind, a win"},
		{"🎰", 64, "seven seven seven: jackpot"},
		{"🎰", 2, "grapes bar bar: no win"},
	}
	for _, c := range cases {
		if got := diceOutcome(c.emoji, c.value); got != c.want {
			t.Errorf("diceOutcome(%s, %d) = %q, want %q", c.emoji, c.value, got, c.want)
		}
	}
}
