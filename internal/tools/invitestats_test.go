package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

type fakeInvites struct{ asked []storage.InviteQuery }

func (f *fakeInvites) InviteTop(_ context.Context, _ int64, q storage.InviteQuery) ([]storage.InviterCount, error) {
	f.asked = append(f.asked, q)
	return []storage.InviterCount{{UserID: 7, Name: "Bek", Added: 5, Stayed: 4}}, nil
}

// The tool reads the week's top by default, the asker's own count, and the
// member replied to, from Monday in the group's zone.
func TestInviteStats(t *testing.T) {
	store := &fakeInvites{}
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, tashkent) // a Wednesday
	registry := mustRegistry(t, InviteStatsTool(InviteStatsOptions{Store: store, Location: tashkent, Now: func() time.Time { return now }}))
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, Target: bekTarget()}, &Sink{})
	run := func(args string) string {
		return registry.Execute(ctx, userID, "invite_stats", args)
	}
	if out := run(`{}`); !strings.Contains(out, `"brought_still_here":4`) || !strings.Contains(out, `"brought_who_left":1`) {
		t.Errorf("top = %s", out)
	}
	if !store.asked[0].Since.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, tashkent)) || store.asked[0].Inviter != 0 {
		t.Errorf("the week = %+v", store.asked[0])
	}
	run(`{"whose":"mine","period":"all"}`)
	if q := store.asked[1]; q.Inviter != userID || !q.Since.IsZero() {
		t.Errorf("mine = %+v", q)
	}
	run(`{"whose":"replied"}`)
	if q := store.asked[2]; q.Inviter != memberID {
		t.Errorf("replied = %+v", q)
	}
}
