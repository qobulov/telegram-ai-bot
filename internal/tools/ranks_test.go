package tools

import (
	"context"
	"strings"
	"testing"
)

type fakeRanks struct {
	ranks     map[int64]int
	overrides map[string]int
}

func (f *fakeRanks) RankOf(_ context.Context, _, userID int64) (int, error) {
	return f.ranks[userID], nil
}
func (f *fakeRanks) CommandRanks(context.Context, int64) (map[string]int, error) {
	return f.overrides, nil
}

// A member with a rank and no Telegram rights mutes when rank 1 opens mute,
// is told the rank deleting needs, and cannot touch a member of their rank
// or higher; the creator raising delete's rank refuses rank 3 too.
func TestRanksOpenModeration(t *testing.T) {
	const moder = int64(4242)
	env := newModerationEnv(t)
	book := &fakeRanks{ranks: map[int64]int{moder: 2}, overrides: map[string]int{}}
	env.opts.Ranks = book
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	asking := func(update int64, words string) context.Context {
		return WithTurn(context.Background(), Caller{UserID: moder, ChatID: groupA, Human: true, UpdateID: update, MessageID: 50, Target: bekTarget(), Words: words}, &Sink{})
	}
	if said := env.act(t, asking(301, "mut ber"), `{"action":"mute","minutes":10}`); !strings.Contains(said, "yoza olmaydi") {
		t.Errorf("a rank 2 mute = %q", said)
	}
	if got := env.moderate(asking(302, "o'chir"), `{"action":"delete"}`); got.OK || !strings.Contains(got.Error.Message, "needs rank 3") {
		t.Errorf("a rank 2 delete = %+v", got)
	}
	book.ranks[memberID] = 2
	if got := env.moderate(asking(303, "ban"), `{"action":"ban"}`); got.OK || !strings.Contains(got.Error.Message, "rank is 2") {
		t.Errorf("banning a member of the same rank = %+v", got)
	}
	book.ranks[memberID] = 0
	book.overrides["mute"] = 3
	if got := env.moderate(asking(304, "mut"), `{"action":"mute"}`); got.OK || !strings.Contains(got.Error.Message, "needs rank 3") {
		t.Errorf("a mute raised to rank 3 = %+v", got)
	}
}
