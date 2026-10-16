package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/tools"
)

// confirmCall is one answer passed on to moderation.
type confirmCall struct {
	caller   tools.Caller
	promptID int64
	yes      bool
}

// fakeConfirmer answers every confirmation with result, and records them.
type fakeConfirmer struct {
	mu     sync.Mutex
	calls  []confirmCall
	result tools.Confirmation
	err    error
}

func (f *fakeConfirmer) Confirm(_ context.Context, caller tools.Caller, promptID int64, yes bool) (tools.Confirmation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, confirmCall{caller: caller, promptID: promptID, yes: yes})
	return f.result, f.err
}

func (f *fakeConfirmer) recorded() []confirmCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]confirmCall(nil), f.calls...)
}

// withConfirmer gives the fixture moderation that finds an action waiting
// and reports it done, and a group quota already used up.
func withConfirmer(f *fixture) *fakeConfirmer {
	confirmer := &fakeConfirmer{result: tools.Confirmation{Found: true, Text: "Done: Bek @bek is banned."}}
	f.opts.Moderation = confirmer
	f.opts.GroupReplies = &fakeGroupQuota{err: errors.New("no answers left")}
	return confirmer
}

func TestYesToTheBotsQuestionIsHandledInCode(t *testing.T) {
	// "ha", "+" and 👍 are what admins answer with; as replies to the bot
	// they would be applause, and the group's quota may be used up.
	for _, answer := range []string{"ha", "+", "👍", "Да!", "yo'q"} {
		t.Run(answer, func(t *testing.T) {
			f := newFixture()
			confirmer := withConfirmer(f)
			update := inGroup(asAdmin(replyingTo(groupMessage(answer), botID, "Confirm: ban Bek @bek? Reply yes")))

			sent := f.run(update)

			expectOneReply(t, sent, "Done: Bek @bek is banned.")
			if sent[0].replyTo != 30 {
				t.Errorf("reply to %d, want the answer 30", sent[0].replyTo)
			}
			_, yes := tools.ConfirmationAnswer(answer)
			calls := confirmer.recorded()
			if len(calls) != 1 || calls[0].promptID != 29 || calls[0].yes != (answer != "yo'q") || !yes {
				t.Fatalf("confirmations = %+v, want one answer to the question 29", calls)
			}
			c := calls[0].caller
			if c.UserID != groupAdminID || c.ChatID != groupID || !c.Human || c.UpdateID != update.UpdateID {
				t.Errorf("caller = %+v, want the admin in this update", c)
			}
			if len(f.replier.recorded()) != 0 || len(f.reactor.recorded()) != 0 {
				t.Errorf("model calls = %d, reactions = %+v; a confirmation needs neither", len(f.replier.recorded()), f.reactor.recorded())
			}
			wantLog := []string{"Boss: (to You) " + answer, "You: Done: Bek @bek is banned."}
			if got := f.chatLog.texts(groupID); !reflect.DeepEqual(got, wantLog) {
				t.Errorf("log = %q, want %q", got, wantLog)
			}
		})
	}
}

func TestAnswerWithNothingWaitingIsAnOrdinaryMessage(t *testing.T) {
	f := newFixture()
	confirmer := withConfirmer(f)
	confirmer.result = tools.Confirmation{}
	f.opts.GroupReplies = nil

	sent := f.run(inGroup(replyingTo(groupMessage("yes"), botID, "Are you ready?")))

	if len(confirmer.recorded()) != 1 {
		t.Errorf("confirmations = %+v, want the answer looked up", confirmer.recorded())
	}
	expectOneReply(t, sent, "Bek: yes")
}

func TestRedeliveredConfirmationSaysNothing(t *testing.T) {
	f := newFixture()
	confirmer := withConfirmer(f)
	confirmer.result = tools.Confirmation{Found: true}

	sent := f.run(inGroup(asAdmin(replyingTo(groupMessage("ha"), botID, "Confirm: ban Bek @bek?"))))

	if len(sent) != 0 || len(f.replier.recorded()) != 0 || len(f.reactor.recorded()) != 0 {
		t.Errorf("sent = %+v, model calls = %d, reactions = %+v; want nothing", sent, len(f.replier.recorded()), f.reactor.recorded())
	}
}

func TestOnlyAPlainAnswerToTheBotIsAConfirmation(t *testing.T) {
	for name, msg := range map[string]struct {
		text    string
		replied int64
	}{
		"a yes to a member":       {"ha", userID},
		"more than the answer":    {"ha, lekin uni emas", botID},
		"a question to the bot":   {"ha?", botID},
		"a reply with no answer":  {"Mira, salom", botID},
		"a sticker to a question": {"", botID},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			confirmer := withConfirmer(f)
			f.opts.GroupReplies = nil
			message := replyingTo(groupMessage(msg.text), msg.replied, "Confirm: ban Bek @bek?")
			if msg.text == "" {
				withSticker(message)
			}

			f.run(inGroup(asAdmin(message)))

			if calls := confirmer.recorded(); len(calls) != 0 {
				t.Errorf("confirmations = %+v, want none", calls)
			}
		})
	}
}

func TestConfirmationFailureIsLoggedAndToldPlainly(t *testing.T) {
	f := newFixture()
	confirmer := withConfirmer(f)
	confirmer.result = tools.Confirmation{Found: true, Text: "Not done: something went wrong on my side; ask again later."}
	confirmer.err = errors.New("storage: count moderation: connection reset")
	var logs bytes.Buffer
	f.opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	sent := f.run(inGroup(asAdmin(replyingTo(groupMessage("ha"), botID, "Confirm: ban Bek @bek?"))))

	expectOneReply(t, sent, "Not done: something went wrong on my side")
	if !strings.Contains(logs.String(), "connection reset") {
		t.Errorf("logs = %q, want the cause", logs.String())
	}
}
