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

// fakeNotices is the daily allowance of one message per group.
type fakeNotices struct {
	mu       sync.Mutex
	taken    map[int64]bool
	released []int64
	next     int64
}

func (f *fakeNotices) ReserveOwnerNotice(_ context.Context, chatID, _ int64, _ time.Time) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.taken[chatID] {
		return 0, false, nil
	}
	f.taken[chatID] = true
	f.next++
	return f.next, true, nil
}

func (f *fakeNotices) ReleaseOwnerNotice(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	for chat := range f.taken {
		delete(f.taken, chat)
	}
	return nil
}

// fakeOwnerChats knows each owner's username and the group's title, and
// records what was sent.
type fakeOwnerChats struct {
	mu      sync.Mutex
	chats   map[int64]telegram.Chat
	blocked map[int64]bool
	sent    []telegram.Outgoing
}

func (f *fakeOwnerChats) GetChat(_ context.Context, chatID int64) (telegram.Chat, error) {
	if chat, ok := f.chats[chatID]; ok {
		return chat, nil
	}
	return telegram.Chat{}, errors.New("chat not found")
}

func (f *fakeOwnerChats) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked[msg.ChatID] {
		return 0, &telegram.APIError{Code: 403, Description: "Forbidden: bot can't initiate conversation with a user"}
	}
	f.sent = append(f.sent, msg)
	return 1, nil
}

const ownerA, ownerB = int64(9101), int64(9102)

type notifyEnv struct {
	notices *fakeNotices
	chats   *fakeOwnerChats
	reg     *Registry
}

func newNotifyEnv(t *testing.T) *notifyEnv {
	t.Helper()
	env := &notifyEnv{
		notices: &fakeNotices{taken: map[int64]bool{}},
		chats: &fakeOwnerChats{chats: map[int64]telegram.Chat{
			ownerA: {ID: ownerA, Type: "private", Username: "pronium"},
			ownerB: {ID: ownerB, Type: "private", Username: "TursunoffM"},
			groupA: {ID: groupA, Type: "supergroup", Title: "Do'stlar"},
		}, blocked: map[int64]bool{}},
	}
	env.reg = mustRegistry(t, NotifyOwnerTool(NotifyOptions{Quota: env.notices, Telegram: env.chats, Owners: []int64{ownerA, ownerB}, Location: tashkent, Now: func() time.Time { return rulesNow }}))
	return env
}

func (env *notifyEnv) notify(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var got envelope
	_ = json.Unmarshal([]byte(env.reg.Execute(ctx, caller.UserID, "notify_owner", args)), &got)
	return got
}

func notifying(words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Name: "Muhammadqodir @mq", Human: true, UpdateID: 100, MessageID: 50, Words: words}, &Sink{})
}

// The asker's own words go to the named owner, with the group and who
// asked, never words the model chose.
func TestNotifyOwnerSendsTheAskersWordsToTheNamedOwner(t *testing.T) {
	env := newNotifyEnv(t)
	got := env.notify(notifying("Мариа @pronium га ёз лимит бўйича сўров тушяпти"), `{"owner":"@Pronium"}`)
	if !got.OK || !strings.Contains(string(got.Result), `"sent":["@pronium"]`) {
		t.Fatalf("notify = %+v %s", got, got.Result)
	}
	if len(env.chats.sent) != 1 {
		t.Fatalf("sent = %+v, want one message", env.chats.sent)
	}
	msg := env.chats.sent[0]
	if msg.ChatID != ownerA || !strings.Contains(msg.Text, "Do'stlar") || !strings.Contains(msg.Text, "Muhammadqodir @mq") || !strings.Contains(msg.Text, "лимит бўйича сўров тушяпти") {
		t.Errorf("message = %+v", msg)
	}
}

func TestNotifyOwnerWithoutANameGoesToEveryOwner(t *testing.T) {
	env := newNotifyEnv(t)
	if got := env.notify(notifying("egalarga ayt: guruhda reklama ko'p"), `{}`); !got.OK || len(env.chats.sent) != 2 {
		t.Errorf("notify = %+v %s, sent %d; want both owners", got, got.Result, len(env.chats.sent))
	}
}

// One message a group a day, as the owner asked.
func TestNotifyOwnerIsOnceADayPerGroup(t *testing.T) {
	env := newNotifyEnv(t)
	env.notify(notifying("birinchi"), `{"owner":"pronium"}`)
	got := env.notify(notifying("ikkinchi"), `{"owner":"pronium"}`)
	if got.OK || !strings.Contains(got.Error.Message, "once a day") || len(env.chats.sent) != 1 {
		t.Errorf("second = %+v, sent %d; want refused and only the first sent", got, len(env.chats.sent))
	}
}

func TestNotifyOwnerRefusals(t *testing.T) {
	t.Run("not an owner", func(t *testing.T) {
		env := newNotifyEnv(t)
		got := env.notify(notifying("yoz"), `{"owner":"@stranger"}`)
		if got.OK || len(env.chats.sent) != 0 || len(env.notices.taken) != 0 {
			t.Errorf("notify = %+v; want refused before the day's message is taken", got)
		}
	})
	t.Run("the owner never started the bot", func(t *testing.T) {
		env := newNotifyEnv(t)
		env.chats.blocked[ownerA] = true
		got := env.notify(notifying("yoz"), `{"owner":"pronium"}`)
		if got.OK || !strings.Contains(got.Error.Message, "/start") || len(env.notices.released) != 1 {
			t.Errorf("notify = %+v, released %v; want the reason and the day's message given back", got, env.notices.released)
		}
	})
	t.Run("private chat", func(t *testing.T) {
		env := newNotifyEnv(t)
		private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Human: true, UpdateID: 100, Words: "yoz"}, &Sink{})
		if got := env.notify(private, `{}`); got.OK {
			t.Errorf("notify = %+v; want refused outside a group", got)
		}
	})
	t.Run("nothing to pass on", func(t *testing.T) {
		env := newNotifyEnv(t)
		if got := env.notify(notifying("  "), `{}`); got.OK {
			t.Errorf("notify = %+v; want refused with no words", got)
		}
	})
}
