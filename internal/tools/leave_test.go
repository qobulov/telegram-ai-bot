package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeLeaver records what it posted and which chats it left.
type fakeLeaver struct {
	posted map[int64]string
	left   []int64
}

func (f *fakeLeaver) SendToGroup(_ context.Context, chatID int64, text string) error {
	if f.posted == nil {
		f.posted = map[int64]string{}
	}
	f.posted[chatID] = text
	return nil
}

func (f *fakeLeaver) LeaveChat(_ context.Context, chatID int64) error {
	f.left = append(f.left, chatID)
	return nil
}

func leave(registry *Registry, caller Caller, sink *Sink, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(WithTurn(context.Background(), caller, sink), caller.UserID, "leave_chat", args)), &env)
	return env
}

// Asked in a group, the bot says goodbye, leaves and ends the turn with
// nothing more; from a private chat it leaves a group or channel it knows
// by chat_id. It leaves only when the owner's own words ask it to, and is
// no tool of a member's.
func TestLeaveTool(t *testing.T) {
	telegram := &fakeLeaver{}
	registry := mustRegistry(t, LeaveTool(LeaveOptions{Groups: &groupsWithStatus{}, Channels: fakeChannels{}, Telegram: telegram}))
	owner := Caller{UserID: userID, ChatID: groupC, Owner: true, UpdateID: 1}

	owner.Words = "Maria, salom"
	if env := leave(registry, owner, &Sink{}, `{}`); env.OK || len(telegram.left) != 0 {
		t.Fatalf("left unasked: %+v", env)
	}

	owner.Words = "Maria bu guruhdan chiqib ket"
	sink := &Sink{}
	env := leave(registry, owner, sink, `{}`)
	if text, done := sink.Finished(); !env.OK || len(telegram.left) != 1 || telegram.left[0] != groupC || !strings.Contains(telegram.posted[groupC], "Xayr") || !done || text != silentAnswer {
		t.Fatalf("leaving here: %+v, left %v, posted %v, finished %q", env, telegram.left, telegram.posted, text)
	}

	private := Caller{UserID: userID, ChatID: userID, Owner: true, UpdateID: 2, Words: "Devs guruhidan chiq"}
	if env := leave(registry, private, &Sink{}, `{"chat_id":-100111}`); !env.OK || telegram.left[1] != groupA || telegram.posted[groupA] != "" {
		t.Errorf("leaving Devs from private: %+v, left %v", env, telegram.left)
	}
	if env := leave(registry, private, &Sink{}, `{"chat_id":-1009001}`); !env.OK || telegram.left[2] != channelNews {
		t.Errorf("leaving the channel: %+v, left %v", env, telegram.left)
	}
	for _, args := range []string{`{}`, `{"chat_id":-100222}`, `{"chat_id":-555}`} {
		if env := leave(registry, private, &Sink{}, args); env.OK {
			t.Errorf("%s from private left: %+v", args, env)
		}
	}

	member := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupC, UpdateID: 3}, &Sink{})
	if defs := registry.Definitions(member, userID); len(defs) != 0 {
		t.Errorf("a member is offered %+v", defs)
	}
}

func TestAskedToLeave(t *testing.T) {
	for words, want := range map[string]bool{
		"Maria, bu guruhdan chiqib ket": true,
		"Devs guruhini tark et":         true,
		"Мария, уходи из этой группы":   true,
		"выйди отсюда":                  true,
		"Maria, go away":                true,
		"exit this chat":                true,
		"Maria, salom":                  false,
		"bugungi ob-havo qanday":        false,
	} {
		if got := askedToLeave(words); got != want {
			t.Errorf("askedToLeave(%q) = %v; want %v", words, got, want)
		}
	}
}
