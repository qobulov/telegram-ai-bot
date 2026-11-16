package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeBans struct {
	banned []storage.BannedMember
	quiet  []storage.GroupMember
}

func (f *fakeBans) BannedMembers(context.Context, int64, int) ([]storage.BannedMember, error) {
	return f.banned, nil
}
func (f *fakeBans) InactiveMembers(context.Context, int64, time.Time, int) ([]storage.GroupMember, error) {
	return f.quiet, nil
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, name) {
			n++
		}
	}
	return n
}

// Amnesty unbans everyone on the list; the inactive are listed first, an
// admin among them spared, and removed only when confirmed.
func TestAmnestyAndTheInactiveKick(t *testing.T) {
	env := newModerationEnv(t)
	env.opts.Bans = &fakeBans{
		banned: []storage.BannedMember{{UserID: 70}, {UserID: 71}},
		quiet:  []storage.GroupMember{{UserID: 80, FirstName: "Quiet"}, {UserID: 81, FirstName: "Boss"}},
	}
	env.chat.set(81, admin(telegram.RightRestrictMembers))
	env.moderation = NewModeration(env.opts)
	env.registry = mustRegistry(t, env.moderation.Tool())
	ask := func(update int64, words string) context.Context {
		return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: update, MessageID: 50, Words: words}, &Sink{})
	}
	if said := env.act(t, ask(401, "/amnesty"), `{"action":"amnesty"}`); !strings.Contains(said, "2 kishi") {
		t.Errorf("amnesty = %q", said)
	}
	if n := countCalls(env.telegram.made(), "unbanChatMember"); n != 2 {
		t.Errorf("unbans = %d", n)
	}
	got := env.moderate(ask(402, "/kickinactive 30"), `{"action":"kick_inactive","days":30}`)
	if !got.OK || !strings.Contains(string(got.Result), "Quiet") || strings.Contains(string(got.Result), "Boss") {
		t.Fatalf("the list = %+v %s", got, got.Result)
	}
	if n := countCalls(env.telegram.made(), "banChatMember"); n != 0 {
		t.Errorf("kicked before the confirmation: %d", n)
	}
	if said := env.act(t, ask(403, "/kickinactive 30 ha"), `{"action":"kick_inactive","days":30,"force":true}`); !strings.Contains(said, "1 kishi") {
		t.Errorf("the kick = %q", said)
	}
	if n := countCalls(env.telegram.made(), "banChatMember"); n != 1 {
		t.Errorf("kicked %d, want the one not an admin", n)
	}
}
