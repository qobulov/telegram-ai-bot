package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

func (env *pollEnv) quiz(t *testing.T, ctx context.Context, args string) envelope {
	t.Helper()
	registry := mustRegistry(t, QuizTool(env.opts))
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "create_quiz", args)), &result)
	return result
}

const twoQuestions = `{"questions":[
	{"question":" Ling qaysi rol? ","options":["Assassin","Tank"],"correct":0,"explanation":"Ling jangl assassin."},
	{"question":"Fanny nima bilan uchadi?","options":["Kabel","Qanot","Ot"],"correct":0}]}`

func TestQuizSentQuestionByQuestion(t *testing.T) {
	env := newPollEnv(t)

	got := env.quiz(t, memberAsking(groupA, 100), twoQuestions)

	if !got.OK || !strings.Contains(string(got.Result), `"sent":2`) {
		t.Fatalf("create_quiz = %+v %s", got, got.Result)
	}
	sent := env.sender.sent()
	if len(sent) != 2 || !sent[0].Quiz || sent[0].Question != "Ling qaysi rol?" || sent[0].Correct != 0 || sent[0].Explanation != "Ling jangl assassin." || sent[0].ReplyToMessageID != 60 {
		t.Fatalf("first = %+v", sent)
	}
	if sent[1].ReplyToMessageID != 0 || !slices.Equal(sent[1].Options, []string{"Kabel", "Qanot", "Ot"}) {
		t.Errorf("second = %+v, want it to follow the first, not reply", sent[1])
	}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != "quiz" {
		t.Errorf("audit = %+v", audit)
	}
}

// A quiz is paced like a poll, but apart from polls.
func TestQuizOncePerTenMinutes(t *testing.T) {
	env := newPollEnv(t)
	if got := env.poll(memberAsking(groupA, 99), lunchPoll); !got.OK {
		t.Fatalf("poll = %+v", got)
	}
	if got := env.quiz(t, memberAsking(groupA, 100), twoQuestions); !got.OK {
		t.Fatalf("quiz after a poll = %+v", got)
	}
	env.now = rulesNow.Add(5 * time.Minute)
	got := env.quiz(t, memberAsking(groupA, 101), twoQuestions)
	if got.OK || got.Error.Message != quizTooSoon {
		t.Errorf("second quiz = %+v, want refused", got)
	}
}

func TestQuizValidation(t *testing.T) {
	env := newPollEnv(t)
	for args, word := range map[string]string{
		`{"questions":[]}`: "questions",
		`{"questions":[{"question":"Q","options":["a","b"],"correct":2}]}`:                                                  "correct",
		`{"questions":[{"question":"Q","options":["a","b"],"correct":-1}]}`:                                                 "correct",
		`{"questions":[{"question":"","options":["a","b"],"correct":0}]}`:                                                   "question 1",
		`{"questions":[{"question":"Q","options":["a","A"],"correct":0}]}`:                                                  "different",
		`{"questions":[{"question":"Q","options":["a","b"],"correct":0,"explanation":"` + strings.Repeat("x", 201) + `"}]}`: "explanation",
	} {
		got := env.quiz(t, memberAsking(groupA, 100), args)
		if got.OK || !strings.Contains(got.Error.Message, word) {
			t.Errorf("%s: %+v, want an error about %q", args, got, word)
		}
	}
	if len(env.sender.sent()) != 0 {
		t.Error("an invalid quiz was sent")
	}
}

// A quiz Telegram refuses at once is forgotten; one cut off later says how
// much went out.
func TestQuizRefusedByTelegram(t *testing.T) {
	env := newPollEnv(t)
	env.sender.err = &telegram.APIError{Code: 400, Description: "Bad Request: not enough rights to send polls to the chat"}
	if got := env.quiz(t, memberAsking(groupA, 100), twoQuestions); got.OK {
		t.Errorf("refused quiz = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("audit = %+v, want the refused quiz forgotten", audit)
	}

	env = newPollEnv(t)
	env.opts.Sender = &failAfter{sender: env.sender, after: 1}
	got := env.quiz(t, memberAsking(groupA, 100), twoQuestions)
	if !got.OK || !strings.Contains(string(got.Result), `"sent":1`) {
		t.Errorf("cut-off quiz = %+v %s", got, got.Result)
	}
}

// failAfter sends the first after polls and refuses the rest.
type failAfter struct {
	sender *fakePollSender
	after  int
}

func (f *failAfter) SendPoll(ctx context.Context, poll telegram.OutgoingPoll) (int64, error) {
	if len(f.sender.sent()) >= f.after {
		return 0, &telegram.APIError{Code: 400, Description: "Bad Request: poll refused"}
	}
	return f.sender.SendPoll(ctx, poll)
}

// A student's quiz in a private chat: up to ten questions, with no pacing
// but the chat's daily limit.
func TestQuizInAPrivateChat(t *testing.T) {
	env := newPollEnv(t)
	var questions []string
	for i := range 10 {
		questions = append(questions, fmt.Sprintf(`{"question":"Savol %d","options":["a","b"],"correct":1}`, i+1))
	}
	ten := `{"questions":[` + strings.Join(questions, ",") + `]}`

	for update := int64(200); update < 203; update++ {
		if got := env.quiz(t, memberAsking(memberID, update), ten); !got.OK {
			t.Fatalf("quiz %d = %+v", update, got)
		}
	}
	if sent := env.sender.sent(); len(sent) != 30 || sent[0].ChatID != memberID || sent[9].Question != "Savol 10" {
		t.Errorf("sent %d, first %+v", len(sent), sent[0])
	}
	eleven := `{"questions":[` + strings.Join(append(questions, questions[0]), ",") + `]}`
	if got := env.quiz(t, memberAsking(memberID, 300), eleven); got.OK || !strings.Contains(got.Error.Message, "1 to 10") {
		t.Errorf("eleven questions = %+v", got)
	}
}
