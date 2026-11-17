package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

type fakeWarns struct{ n map[int64]int }

func (f *fakeWarns) AddWarn(_ context.Context, _, userID int64, _ string) (int, error) {
	f.n[userID]++
	return f.n[userID], nil
}
func (f *fakeWarns) RemoveWarn(_ context.Context, _, userID int64) (int, error) {
	f.n[userID] = max(0, f.n[userID]-1)
	return f.n[userID], nil
}
func (f *fakeWarns) ResetWarns(_ context.Context, _, userID int64) error {
	f.n[userID] = 0
	return nil
}

// A member warned twice is told so; the third warning mutes them for a day
// and clears the count; unwarn takes one off; the same message never warns
// twice.
func TestWarnMutesAtTheThird(t *testing.T) {
	env := newModerationEnv(t)
	warns := &fakeWarns{n: map[int64]int{}}
	env.opts.Warns = warns
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	warn := func(update int64, words string) string {
		ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: update, MessageID: 50, Target: bekTarget(), Words: words}, &Sink{})
		return env.act(t, ctx, `{"action":"warn"}`)
	}
	if said := warn(201, "varn ber"); !strings.Contains(said, "1/3") {
		t.Errorf("first = %q", said)
	}
	if said := warn(201, "varn ber"); said != "" || warns.n[memberID] != 1 {
		t.Errorf("the same message again = %q, count %d", said, warns.n[memberID])
	}
	if said := warn(202, "пред"); !strings.Contains(said, "2/3") || len(env.telegram.restricted()) != 0 {
		t.Errorf("second = %q, restrictions %+v", said, env.telegram.restricted())
	}
	said := warn(203, "warn")
	r := env.telegram.restricted()
	if !strings.Contains(said, "3/3") || len(r) != 1 || r[0].Permissions != telegram.NoPermissions() || !r[0].Until.Equal(rulesNow.Add(24*time.Hour)) || warns.n[memberID] != 0 {
		t.Fatalf("third = %q, restrictions %+v, count %d", said, r, warns.n[memberID])
	}
	warns.n[memberID] = 2
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 204, MessageID: 50, Target: bekTarget(), Words: "varnni olib tashla"}, &Sink{})
	if said := env.act(t, ctx, `{"action":"unwarn"}`); !strings.Contains(said, "1/3") {
		t.Errorf("unwarn = %q", said)
	}
}
