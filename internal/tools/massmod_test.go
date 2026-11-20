package tools

import (
	"context"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
)

type fakeBans struct {
	banned []storage.BannedMember
}

func (f *fakeBans) BannedMembers(context.Context, int64, int) ([]storage.BannedMember, error) {
	return f.banned, nil
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

// Amnesty unbans everyone on the list.
func TestAmnesty(t *testing.T) {
	env := newModerationEnv(t)
	env.opts.Bans = &fakeBans{
		banned: []storage.BannedMember{{UserID: 70}, {UserID: 71}},
	}
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
}
