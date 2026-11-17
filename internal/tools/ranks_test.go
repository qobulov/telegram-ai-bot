package tools

import (
	"context"
	"encoding/json"
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

type rankWrites struct{ fakeRanks }

func (r *rankWrites) SetRank(_ context.Context, _, userID int64, rank int, _ int64) error {
	if rank == 0 {
		delete(r.ranks, userID)
	} else {
		r.ranks[userID] = rank
	}
	return nil
}

// set_rank does what "+3 admin" does for wordings the code missed: a rank 4
// moderator gives and takes ranks below their own, a lower one is told the
// rank it needs, and an owner gives any.
func TestSetRankTool(t *testing.T) {
	const moder = int64(4242)
	env := newModerationEnv(t)
	book := &rankWrites{fakeRanks{ranks: map[int64]int{moder: 4}, overrides: map[string]int{}}}
	env.opts.Ranks = book
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.RankTool(book))
	run := func(user int64, owner bool, words, args string) envelope {
		ctx := WithTurn(context.Background(), Caller{UserID: user, ChatID: groupA, Human: true, Owner: owner, UpdateID: 800, MessageID: 50, Target: bekTarget(), Words: words}, &Sink{})
		var result envelope
		_ = json.Unmarshal([]byte(env.registry.Execute(ctx, user, "set_rank", args)), &result)
		return result
	}
	if got := run(moder, false, "- админ Мари", `{"rank":2}`); !got.OK || book.ranks[memberID] != 2 {
		t.Fatalf("a rank 4's give = %+v, ranks %v", got, book.ranks)
	}
	if got := run(moder, false, "4 daraja ber", `{"rank":4}`); got.OK {
		t.Errorf("gave their own rank: %+v", got)
	}
	if got := run(moder, false, "darajasini olib tashla", `{"rank":0}`); !got.OK || book.ranks[memberID] != 0 {
		t.Errorf("take = %+v, ranks %v", got, book.ranks)
	}
	book.ranks[moder] = 2
	if got := run(moder, false, "1 daraja ber", `{"rank":1}`); got.OK || !strings.Contains(got.Error.Message, "needs rank 4") {
		t.Errorf("a rank 2's give = %+v", got)
	}
	if got := run(userID, true, "5 daraja ber", `{"rank":5}`); !got.OK || book.ranks[memberID] != 5 {
		t.Errorf("an owner's give = %+v", got)
	}
	if got := run(userID, true, "salom", `{"rank":1}`); got.OK {
		t.Errorf("a rank given unasked: %+v", got)
	}
}
