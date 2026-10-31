package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeDrafts struct {
	mu    sync.Mutex
	draft *storage.Broadcast
	used  bool
}

func (f *fakeDrafts) SaveBroadcast(_ context.Context, b storage.Broadcast) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.draft, f.used = &b, false
	return nil
}

func (f *fakeDrafts) ConfirmBroadcast(_ context.Context, ownerID, updateID int64, _ time.Duration) (storage.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.draft == nil || f.used || f.draft.OwnerID != ownerID || f.draft.UpdateID == updateID {
		return storage.Broadcast{}, storage.ErrNoBroadcast
	}
	f.used = true
	return *f.draft, nil
}

func (f *fakeDrafts) CancelBroadcast(_ context.Context, ownerID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	had := f.draft != nil && !f.used && f.draft.OwnerID == ownerID
	f.draft = nil
	return had, nil
}

type fakeGroupSender struct {
	mu   sync.Mutex
	sent map[int64]string
	fail map[int64]bool
}

func (f *fakeGroupSender) SendToGroup(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[chatID] {
		return errors.New("telegram: 403 Forbidden: bot was kicked")
	}
	if f.sent == nil {
		f.sent = map[int64]string{}
	}
	f.sent[chatID] = text
	return nil
}

// CopyToChat records a copy as "copy of <message> from <chat>", and its
// new caption after a colon.
func (f *fakeGroupSender) CopyToChat(ctx context.Context, chatID, fromChatID, messageID int64, caption string) error {
	copied := fmt.Sprintf("copy of %d from %d", messageID, fromChatID)
	if caption != "" {
		copied += ": " + caption
	}
	return f.SendToGroup(ctx, chatID, copied)
}

// fakeChannels lists the channel News, where the bot may post, Quiet,
// where it may not, and Gone, which removed it.
type fakeChannels struct{}

const (
	channelNews  = int64(-1009001)
	channelQuiet = int64(-1009002)
	channelGone  = int64(-1009003)
)

func (fakeChannels) Channels(context.Context, int) ([]storage.ChannelSummary, error) {
	post := []string{telegram.RightPostMessages}
	return []storage.ChannelSummary{
		{ChatID: channelNews, Title: "News", BotStatus: "administrator", Rights: post},
		{ChatID: channelQuiet, Title: "Quiet", BotStatus: "administrator", Rights: []string{telegram.RightInviteUsers}},
		{ChatID: channelGone, Title: "Gone", BotStatus: "kicked", Rights: post},
	}, nil
}

func channelRegistry(t *testing.T, drafts *fakeDrafts, sender *fakeGroupSender) *Registry {
	t.Helper()
	return mustRegistry(t, BroadcastTool(BroadcastOptions{Groups: &groupsWithStatus{}, Channels: fakeChannels{}, Drafts: drafts, Sender: sender}))
}

// Channels are posted in only when asked, and only where the bot is an
// admin who may post; chat_ids may pick groups and channels alike.
func TestBroadcastPostsInChannelsWhenAsked(t *testing.T) {
	drafts, sender := &fakeDrafts{}, &fakeGroupSender{}
	registry := channelRegistry(t, drafts, sender)

	env := broadcast(registry, ownerTurn(userID, 1), `{"action":"targets"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"channels":[{"chat_id":-1009001,"name":"News"}]`) || strings.Contains(string(env.Result), "Quiet") {
		t.Errorf("targets = %s", env.Result)
	}
	if env := broadcast(registry, ownerTurn(userID, 2), `{"action":"prepare","text":"Yangilik!"}`); !env.OK || strings.Contains(string(env.Result), "News") {
		t.Errorf("a plain broadcast = %s; want the groups only", env.Result)
	}
	if env := broadcast(registry, ownerTurn(userID, 3), `{"action":"prepare","text":"Yangilik!","to":"channels"}`); !env.OK || !strings.Contains(string(env.Result), `"channels":["News"]`) {
		t.Fatalf("to channels = %s", env.Result)
	}
	if env := broadcast(registry, ownerTurn(userID, 4), `{"action":"confirm"}`); !env.OK || len(sender.sent) != 1 || sender.sent[channelNews] != "Yangilik!" {
		t.Errorf("confirm = %+v, sent = %+v", env, sender.sent)
	}

	broadcast(registry, ownerTurn(userID, 5), `{"action":"prepare","text":"hi","chat_ids":[-100111,-1009001]}`)
	broadcast(registry, ownerTurn(userID, 6), `{"action":"confirm"}`)
	if sender.sent[groupA] != "hi" || sender.sent[channelNews] != "hi" {
		t.Errorf("sent = %+v, want Devs and News", sender.sent)
	}
	for _, args := range []string{
		`{"action":"prepare","text":"hi","chat_ids":[-1009002]}`,
		`{"action":"prepare","text":"hi","to":"everywhere"}`,
	} {
		if env := broadcast(registry, ownerTurn(userID, 7), args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%s = %+v", args, env)
		}
	}
	both := broadcast(registry, ownerTurn(userID, 8), `{"action":"prepare","text":"hi","to":"both"}`)
	if !both.OK || !strings.Contains(string(both.Result), "Devs") || !strings.Contains(string(both.Result), "News") {
		t.Errorf("to both = %s", both.Result)
	}
}

// Without channels to post in, asking for them says why.
func TestBroadcastToChannelsNeedsOne(t *testing.T) {
	registry := broadcastRegistry(t, &fakeDrafts{}, &fakeGroupSender{})
	if env := broadcast(registry, ownerTurn(userID, 1), `{"action":"prepare","text":"hi","to":"channels"}`); env.OK || !strings.Contains(env.Error.Message, "right to post messages") {
		t.Errorf("prepare = %+v", env)
	}
}

// With copy, the message the owner replied to is posted as it is, or under
// a new caption, after their yes like any broadcast, the owner seeing it
// first in their private chat; it too may not call anyone.
func TestBroadcastCopiesTheRepliedMessage(t *testing.T) {
	drafts, sender := &fakeDrafts{}, &fakeGroupSender{}
	registry := channelRegistry(t, drafts, sender)
	replying := func(updateID int64, target *Target) context.Context {
		return WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, UpdateID: updateID, Target: target}, &Sink{})
	}
	post := &Target{MessageID: 31, UserID: userID, Text: "Turnir posteri"}

	env := broadcast(registry, replying(1, post), `{"action":"prepare","copy":true,"to":"both"}`)
	want := fmt.Sprintf("copy of 31 from %d", userID)
	if !env.OK || !strings.Contains(string(env.Result), "replied to") || strings.Contains(string(env.Result), `"caption"`) {
		t.Fatalf("prepare = %s", env.Result)
	}
	if len(sender.sent) != 1 || sender.sent[userID] != want {
		t.Fatalf("before confirmation sent %+v, want only the owner's preview", sender.sent)
	}
	broadcast(registry, replying(2, nil), `{"action":"confirm"}`)
	if sender.sent[groupA] != want || sender.sent[groupC] != want || sender.sent[channelNews] != want {
		t.Errorf("sent = %+v, want the message copied everywhere", sender.sent)
	}

	env = broadcast(registry, replying(3, post), `{"action":"prepare","copy":true,"text":"Akkaunt sotiladi: 150$"}`)
	captioned := want + ": Akkaunt sotiladi: 150$"
	if !env.OK || !strings.Contains(string(env.Result), `"caption":"Akkaunt sotiladi: 150$"`) || sender.sent[userID] != captioned {
		t.Fatalf("prepare with a caption = %s, preview %q", env.Result, sender.sent[userID])
	}
	broadcast(registry, replying(4, nil), `{"action":"confirm"}`)
	if sender.sent[groupA] != captioned || sender.sent[groupC] != captioned {
		t.Errorf("sent = %+v, want the message under its new caption", sender.sent)
	}

	for _, c := range []struct {
		target *Target
		args   string
	}{
		{nil, `{"action":"prepare","copy":true}`},
		{post, `{"action":"prepare","copy":true,"text":"` + strings.Repeat("a", maxCaptionRunes+1) + `"}`},
		{post, `{"action":"prepare","copy":true,"text":"@pronium sizni @tursunoffm chaqiryapti"}`},
		{&Target{MessageID: 32, Text: "@pronium sizni @tursunoffm chaqiryapti"}, `{"action":"prepare","copy":true}`},
	} {
		if env := broadcast(registry, replying(5, c.target), c.args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%s with %+v = %+v, want refused", c.args, c.target, env)
		}
	}
}

// groupsWithStatus is a database where the bot is admin in groupA, was
// removed from groupB and is a member of groupC.
type groupsWithStatus struct{ fakeDatabase }

const groupC = int64(-100333)

func (groupsWithStatus) Groups(context.Context, time.Time, int) ([]storage.GroupSummary, error) {
	return []storage.GroupSummary{
		{ChatID: groupA, Title: "Devs", BotStatus: "administrator"},
		{ChatID: groupB, Title: "Old", BotStatus: "kicked"},
		{ChatID: groupC, Title: "Pub"},
	}, nil
}

func broadcastRegistry(t *testing.T, drafts *fakeDrafts, sender *fakeGroupSender) *Registry {
	t.Helper()
	return mustRegistry(t, BroadcastTool(BroadcastOptions{Groups: &groupsWithStatus{}, Drafts: drafts, Sender: sender}))
}

func ownerTurn(chatID, updateID int64) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: chatID, Owner: true, UpdateID: updateID}, &Sink{})
}

func broadcast(registry *Registry, ctx context.Context, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "send_to_groups", args)), &env)
	return env
}

func TestBroadcastIsOnlyForOwners(t *testing.T) {
	registry := broadcastRegistry(t, &fakeDrafts{}, &fakeGroupSender{})
	member := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, UpdateID: 1}, &Sink{})

	if defs := registry.Definitions(member, userID); len(defs) != 0 {
		t.Errorf("a member is offered %+v", defs)
	}
	expectError(t, registry.Execute(member, userID, "send_to_groups", `{"action":"prepare","text":"hi"}`), "forbidden")
}

func TestBroadcastIsSentOnlyAfterTheOwnerConfirms(t *testing.T) {
	drafts, sender := &fakeDrafts{}, &fakeGroupSender{}
	registry := broadcastRegistry(t, drafts, sender)

	env := broadcast(registry, ownerTurn(userID, 1), `{"action":"prepare","text":"Master qaytdi!"}`)
	if !env.OK || !strings.Contains(string(env.Result), "Devs") || !strings.Contains(string(env.Result), "Pub") || strings.Contains(string(env.Result), "Old") {
		t.Fatalf("prepare = %s; want the groups the bot is in", env.Result)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent before confirmation: %+v", sender.sent)
	}
	// The model may not confirm on its own in the same message.
	if env := broadcast(registry, ownerTurn(userID, 1), `{"action":"confirm"}`); env.OK || len(sender.sent) != 0 {
		t.Fatalf("confirmed in the drafting message: %+v, sent %+v", env, sender.sent)
	}

	env = broadcast(registry, ownerTurn(userID, 2), `{"action":"confirm"}`)
	if !env.OK || len(sender.sent) != 2 || sender.sent[groupA] != "Master qaytdi!" || sender.sent[groupC] != "Master qaytdi!" {
		t.Errorf("confirm = %+v, sent = %+v", env, sender.sent)
	}
}

func TestBroadcastToChosenGroupsReportsFailures(t *testing.T) {
	drafts, sender := &fakeDrafts{}, &fakeGroupSender{fail: map[int64]bool{groupC: true}}
	registry := broadcastRegistry(t, drafts, sender)

	if env := broadcast(registry, ownerTurn(userID, 1), `{"action":"prepare","text":"hi","chat_ids":[-100111,-100333]}`); !env.OK {
		t.Fatalf("prepare = %+v", env)
	}
	env := broadcast(registry, ownerTurn(userID, 2), `{"action":"confirm"}`)
	if !env.OK || !strings.Contains(string(env.Result), `"sent":1`) || !strings.Contains(string(env.Result), "Pub") {
		t.Errorf("confirm = %s; want one sent and Pub reported as failed", env.Result)
	}
}

func TestBroadcastChecksItsArguments(t *testing.T) {
	registry := broadcastRegistry(t, &fakeDrafts{}, &fakeGroupSender{})
	for _, args := range []string{
		`{"action":"prepare"}`,
		`{"action":"prepare","text":"` + strings.Repeat("x", 4001) + `"}`,
		`{"action":"prepare","text":"hi","chat_ids":[-999]}`,
		`{"action":"prepare","text":"hi","chat_ids":[-100222]}`,
		`{"action":"shout"}`,
	} {
		if env := broadcast(registry, ownerTurn(userID, 1), args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("%.60s: %+v", args, env)
		}
	}
}

func TestBroadcastCanBeCancelled(t *testing.T) {
	drafts, sender := &fakeDrafts{}, &fakeGroupSender{}
	registry := broadcastRegistry(t, drafts, sender)
	broadcast(registry, ownerTurn(userID, 1), `{"action":"prepare","text":"hi"}`)

	if env := broadcast(registry, ownerTurn(userID, 2), `{"action":"cancel"}`); !env.OK {
		t.Fatalf("cancel = %+v", env)
	}
	if env := broadcast(registry, ownerTurn(userID, 3), `{"action":"confirm"}`); env.OK || len(sender.sent) != 0 {
		t.Errorf("a cancelled broadcast was sent: %+v", sender.sent)
	}
}

// A broadcast that calls someone would tag them in every group: it is
// refused before anything is saved. An announcement naming people is not.
func TestBroadcastRefusesSummons(t *testing.T) {
	for text, refused := range map[string]bool{
		"@pronium sizni @tursunoffm chaqiryapti!":                true,
		"Azizbek, sizni @pronium chaqiryapti!":                   true,
		"@pronium, тебя зовёт @tursunoffm":                       true,
		"Maria'ni @pronium dasturlagan, egasi esa @tursunoffm 🤍": false,
		"Ertaga turnir bor, hammani chaqiramiz!":                 false,
	} {
		if got := summons(text); got != refused {
			t.Errorf("summons(%q) = %v, want %v", text, got, refused)
		}
	}
}
