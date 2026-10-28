package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// fakeGroupLog is a group's log, oldest first.
type fakeGroupLog struct {
	messages []storage.GroupMessage
	err      error
	asked    int64
}

func (f *fakeGroupLog) GroupMessagesSince(_ context.Context, chatID int64, since time.Time, limit int) ([]storage.GroupMessage, error) {
	f.asked = chatID
	var out []storage.GroupMessage
	for _, m := range f.messages {
		if !m.SentAt.Before(since) {
			out = append(out, m)
		}
	}
	return out[max(0, len(out)-limit):], f.err
}

func logged(id, author int64, name, text string, ago time.Duration) storage.GroupMessage {
	return storage.GroupMessage{ChatID: groupA, MessageID: id, AuthorID: author, AuthorName: name, Text: text, SentAt: rulesNow.Add(-ago)}
}

func askCatchUp(t *testing.T, log *fakeGroupLog, ctx context.Context, args string) (envelope, catchUpResult) {
	t.Helper()
	registry := mustRegistry(t, CatchUpTool(CatchUpOptions{Store: log, Location: tashkent, Now: func() time.Time { return rulesNow }}))
	caller, _ := CallerFrom(ctx)
	var got envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "catch_up", args)), &got)
	var result catchUpResult
	if got.OK {
		if err := json.Unmarshal(got.Result, &result); err != nil {
			t.Fatalf("result %s: %v", got.Result, err)
		}
	}
	return got, result
}

// Without hours, the recap starts after the asker's own last message, the
// one being answered aside.
func TestCatchUpStartsAfterTheAskersLastMessage(t *testing.T) {
	log := &fakeGroupLog{messages: []storage.GroupMessage{
		logged(10, memberID, "Bek", "kecha nima bo'ldi", 5*time.Hour),
		logged(11, userID, "Ali", "men ketdim", 4*time.Hour),
		logged(12, memberID, "Bek", "ertaga futbol 18:00 da", 3*time.Hour),
		logged(0, rulesBotID, "You", "zo'r", 3*time.Hour),
		logged(50, userID, "Ali", "Maria nima gaplashdilar", time.Minute),
	}}
	got, result := askCatchUp(t, log, asking(1, nil), `{}`)
	if !got.OK || log.asked != groupA {
		t.Fatalf("got %+v, read chat %d", got, log.asked)
	}
	want := []string{"12:04 Bek: ertaga futbol 18:00 da", "12:04 You: zo'r", "15:03 Ali: Maria nima gaplashdilar"}
	if strings.Join(result.Messages, "|") != strings.Join(want, "|") || result.From != "16 Oct 12:04" {
		t.Errorf("result = %+v", result)
	}
}

func TestCatchUpTakesTheHoursAsked(t *testing.T) {
	log := &fakeGroupLog{messages: []storage.GroupMessage{
		logged(10, memberID, "Bek", "eski gap", 5*time.Hour),
		logged(12, memberID, "Bek", "yangi gap", time.Hour),
	}}
	got, result := askCatchUp(t, log, asking(1, nil), `{"hours":2}`)
	if !got.OK || len(result.Messages) != 1 || !strings.Contains(result.Messages[0], "yangi gap") {
		t.Errorf("got %+v, %+v", got, result)
	}
	for _, args := range []string{`{"hours":25}`, `{"hours":-1}`, `{"chat_id":-5}`} {
		if got, _ := askCatchUp(t, log, asking(1, nil), args); got.OK || got.Error.Code != "invalid_arguments" {
			t.Errorf("%s: %+v", args, got)
		}
	}
}

// A long day keeps its newest messages, and says how many were left out.
func TestCatchUpKeepsTheNewestThatFit(t *testing.T) {
	log := &fakeGroupLog{}
	for i := range 400 {
		log.messages = append(log.messages, logged(int64(100+i), memberID, "Bek", strings.Repeat("<", 150), time.Duration(400-i)*time.Minute))
	}
	got, result := askCatchUp(t, log, asking(1, nil), `{}`)
	if !got.OK {
		t.Fatalf("got %+v", got)
	}
	if left := 400 - len(result.Messages); left == 0 || !strings.Contains(result.Note, "older messages did not fit") {
		t.Errorf("kept %d, note %q", len(result.Messages), result.Note)
	}
	if !strings.Contains(result.Messages[len(result.Messages)-1], "15:03") {
		t.Errorf("the newest message is missing: %q", result.Messages[len(result.Messages)-1])
	}
}

func TestCatchUpOutsideAGroupOrWithoutTheLog(t *testing.T) {
	quiet, result := askCatchUp(t, &fakeGroupLog{}, asking(1, nil), `{}`)
	if !quiet.OK || len(result.Messages) != 0 || result.Note != catchUpEmptyNote {
		t.Errorf("an empty log: %+v, %+v", quiet, result)
	}
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Human: true, UpdateID: 1}, &Sink{})
	if got, _ := askCatchUp(t, &fakeGroupLog{}, private, `{}`); got.OK {
		t.Errorf("a private chat got a recap: %+v", got)
	}
	if got, _ := askCatchUp(t, &fakeGroupLog{err: errors.New("down")}, asking(1, nil), `{}`); got.OK || !strings.Contains(got.Error.Message, "could not be read") {
		t.Errorf("a failed read: %+v", got)
	}
}
