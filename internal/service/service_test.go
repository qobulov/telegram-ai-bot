package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/telegram"
)

const adminID, userID = int64(1), int64(2)

type sentMessage struct {
	chatID int64
	text   string
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (f *fakeSender) SendMessage(ctx context.Context, chatID int64, text string) error {
	if err := ctx.Err(); err != nil {
		return err // like a real client, an expired context sends nothing
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID: chatID, text: text})
	return nil
}

func (f *fakeSender) messages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.sent...)
}

type replyCall struct {
	userID, updateID int64
	text             string
}

type fakeReplier struct {
	mu      sync.Mutex
	calls   []replyCall
	err     error
	started chan struct{} // receives once per call, if set
	release chan struct{} // each call waits for it, if set
	// waitForDeadline makes Reply use up the whole turn timeout.
	waitForDeadline bool
}

func (f *fakeReplier) Reply(ctx context.Context, userID, updateID int64, text string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, replyCall{userID: userID, updateID: updateID, text: text})
	err := f.err
	f.mu.Unlock()

	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}
	if f.waitForDeadline {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return "echo: " + text, nil
}

func (f *fakeReplier) recorded() []replyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]replyCall(nil), f.calls...)
}

type fakeResetter struct {
	mu    sync.Mutex
	reset []int64
	err   error
}

func (f *fakeResetter) Reset(_ context.Context, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reset = append(f.reset, userID)
	return f.err
}

type fixture struct {
	sender   *fakeSender
	replier  *fakeReplier
	resetter *fakeResetter
	opts     Options
}

func newFixture() *fixture {
	f := &fixture{sender: &fakeSender{}, replier: &fakeReplier{}, resetter: &fakeResetter{}}
	f.opts = Options{
		Sender:        f.sender,
		Replier:       f.replier,
		Resetter:      f.resetter,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Model:         "test/model",
		Workers:       2,
		QueueSize:     8,
		RatePerMinute: 10,
		MaxInputChars: 50,
		TurnTimeout:   5 * time.Second,
		IsAdmin:       func(id int64) bool { return id == adminID },
		AdminCommands: map[string]bool{},
	}
	return f
}

// run handles the updates and waits for all of them to be processed.
func (f *fixture) run(updates ...telegram.Update) []sentMessage {
	s := New(f.opts)
	for _, u := range updates {
		s.Handle(u)
	}
	s.stop() // unlike Close, runs everything that is queued
	return f.sender.messages()
}

var nextUpdateID int64

func textFrom(senderID int64, text string) telegram.Update {
	nextUpdateID++
	return telegram.Update{
		UpdateID: nextUpdateID,
		Message: &telegram.Message{
			From: &telegram.User{ID: senderID},
			Chat: telegram.Chat{ID: senderID, Type: telegram.ChatTypePrivate},
			Text: text,
		},
	}
}

func expectOneReply(t *testing.T, sent []sentMessage, want string) {
	t.Helper()
	if len(sent) != 1 {
		t.Fatalf("sent = %+v, want exactly one message", sent)
	}
	if !strings.Contains(sent[0].text, want) {
		t.Errorf("reply = %q, want it to contain %q", sent[0].text, want)
	}
}

func TestCommandsReply(t *testing.T) {
	cases := map[string]string{
		"/start":       "/help",
		"/help":        "/reset",
		"/about":       "OpenRouter",
		"/model":       "test/model",
		"/model@mybot": "test/model",
		"/HELP":        "/reset",
		"/help extra":  "/reset",
		"/unknown":     "/help",
	}
	for command, want := range cases {
		t.Run(command, func(t *testing.T) {
			f := newFixture()
			sent := f.run(textFrom(userID, command))
			expectOneReply(t, sent, want)
			if sent[0].chatID != userID {
				t.Errorf("reply went to chat %d", sent[0].chatID)
			}
			if len(f.replier.recorded()) != 0 {
				t.Error("a command reached the model")
			}
		})
	}
}

func TestResetClearsOnlyTheSendersHistory(t *testing.T) {
	f := newFixture()

	sent := f.run(textFrom(userID, "/reset"))

	expectOneReply(t, sent, textResetDone)
	if len(f.resetter.reset) != 1 || f.resetter.reset[0] != userID {
		t.Errorf("reset calls = %v, want [%d]", f.resetter.reset, userID)
	}
}

func TestResetFailureShowsGenericError(t *testing.T) {
	f := newFixture()
	f.resetter.err = errors.New("pq: connection refused at 10.0.0.5")

	sent := f.run(textFrom(userID, "/reset"))

	expectOneReply(t, sent, textError)
	if strings.Contains(sent[0].text, "10.0.0.5") {
		t.Errorf("reply leaks the internal error: %q", sent[0].text)
	}
}

func TestAdminCommandsAreRestricted(t *testing.T) {
	f := newFixture()
	f.opts.AdminCommands = map[string]bool{"/model": true}

	sent := f.run(textFrom(userID, "/model"), textFrom(adminID, "/model"))

	if len(sent) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	for _, msg := range sent {
		isAdminChat := msg.chatID == adminID
		if isAdminChat != strings.Contains(msg.text, "test/model") {
			t.Errorf("chat %d got %q", msg.chatID, msg.text)
		}
		if !isAdminChat && msg.text != textForbidden {
			t.Errorf("non-admin got %q, want the forbidden text", msg.text)
		}
	}
}

func TestTextIsAnsweredByTheAgent(t *testing.T) {
	f := newFixture()
	update := textFrom(userID, "  salom  ")

	sent := f.run(update)

	expectOneReply(t, sent, "echo: salom")
	calls := f.replier.recorded()
	if len(calls) != 1 || calls[0].userID != userID || calls[0].updateID != update.UpdateID || calls[0].text != "salom" {
		t.Errorf("replier calls = %+v", calls)
	}
}

func TestAgentErrorShowsGenericError(t *testing.T) {
	f := newFixture()
	f.replier.err = errors.New("openrouter: status 401: invalid key sk-or-secret")

	sent := f.run(textFrom(userID, "hi"))

	expectOneReply(t, sent, textError)
	if strings.Contains(sent[0].text, "sk-or-secret") || strings.Contains(sent[0].text, "401") {
		t.Errorf("reply leaks the internal error: %q", sent[0].text)
	}
}

func TestAgentTimeoutShowsTimeoutText(t *testing.T) {
	f := newFixture()
	f.replier.err = context.DeadlineExceeded

	expectOneReply(t, f.run(textFrom(userID, "hi")), textTimeout)
}

func TestTimeoutReplyIsDeliveredAfterTheTurnDeadline(t *testing.T) {
	f := newFixture()
	f.opts.TurnTimeout = 20 * time.Millisecond
	f.replier.waitForDeadline = true

	expectOneReply(t, f.run(textFrom(userID, "hi")), textTimeout)
}

func TestDuplicateUpdateIsSilent(t *testing.T) {
	f := newFixture()
	f.replier.err = agent.ErrDuplicate

	if sent := f.run(textFrom(userID, "hi")); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing", sent)
	}
}

func TestOverlongInputIsRejected(t *testing.T) {
	f := newFixture()

	sent := f.run(textFrom(userID, strings.Repeat("я", 51)))

	expectOneReply(t, sent, "50")
	if len(f.replier.recorded()) != 0 {
		t.Error("over-long input reached the model")
	}
}

func TestInputLimitCountsCharactersNotBytes(t *testing.T) {
	f := newFixture()

	f.run(textFrom(userID, strings.Repeat("я", 50))) // 100 bytes, 50 characters

	if len(f.replier.recorded()) != 1 {
		t.Error("input at the character limit was rejected")
	}
}

func TestNonTextMessageGetsTextOnlyReply(t *testing.T) {
	f := newFixture()

	sent := f.run(textFrom(userID, "")) // a photo, sticker or document has no text

	expectOneReply(t, sent, textTextOnly)
}

func TestIgnoredUpdates(t *testing.T) {
	group := textFrom(userID, "hi")
	group.Message.Chat = telegram.Chat{ID: -100, Type: "group"}
	fromBot := textFrom(userID, "hi")
	fromBot.Message.From.IsBot = true
	anonymous := textFrom(userID, "hi")
	anonymous.Message.From = nil

	cases := map[string]telegram.Update{
		"no message":      {UpdateID: 1},
		"group chat":      group,
		"sent by a bot":   fromBot,
		"no sender":       anonymous,
		"whitespace only": textFrom(userID, " \n\t "),
	}
	for name, update := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			if sent := f.run(update); len(sent) != 0 {
				t.Errorf("sent = %+v, want nothing", sent)
			}
			if len(f.replier.recorded()) != 0 {
				t.Error("the update reached the model")
			}
		})
	}
}

func TestRateLimitWarnsOnceThenStaysSilent(t *testing.T) {
	f := newFixture()
	f.opts.RatePerMinute = 3
	f.opts.Workers = 1
	var updates []telegram.Update
	for range 6 {
		updates = append(updates, textFrom(userID, "hi"))
	}

	sent := f.run(updates...)

	if calls := f.replier.recorded(); len(calls) != 3 {
		t.Errorf("model calls = %d, want 3", len(calls))
	}
	warnings := 0
	for _, msg := range sent {
		if msg.text == textRateLimited {
			warnings++
		}
	}
	if warnings != 1 || len(sent) != 4 {
		t.Errorf("sent = %+v, want 3 answers and exactly 1 rate-limit warning", sent)
	}
}

func TestRateLimitIsPerUser(t *testing.T) {
	f := newFixture()
	f.opts.RatePerMinute = 1

	f.run(textFrom(userID, "hi"), textFrom(adminID, "hi"))

	if calls := f.replier.recorded(); len(calls) != 2 {
		t.Errorf("model calls = %d, want 2", len(calls))
	}
}

func TestFullQueueGetsBusyReply(t *testing.T) {
	f := newFixture()
	f.opts.Workers = 1
	f.opts.QueueSize = 1
	f.replier.started = make(chan struct{}, 8)
	f.replier.release = make(chan struct{})
	s := New(f.opts)

	s.Handle(textFrom(userID, "first"))
	<-f.replier.started // the worker is now busy with "first"
	s.Handle(textFrom(userID, "second"))
	s.Handle(textFrom(userID, "third")) // queue is full

	close(f.replier.release)
	s.stop() // unlike Close, runs everything that is queued

	busy := 0
	for _, msg := range f.sender.messages() {
		if msg.text == textBusy {
			busy++
		}
	}
	if busy != 1 {
		t.Errorf("sent = %+v, want exactly one busy reply", f.sender.messages())
	}
	if calls := f.replier.recorded(); len(calls) != 2 {
		t.Errorf("model calls = %d, want 2 (the third update is dropped)", len(calls))
	}
}

func TestMessagesFromOneUserAreProcessedInOrder(t *testing.T) {
	f := newFixture()
	f.opts.Workers = 4
	f.opts.RatePerMinute = 100
	f.opts.QueueSize = 32
	var updates []telegram.Update
	for _, text := range []string{"1", "2", "3", "4", "5", "6", "7", "8"} {
		updates = append(updates, textFrom(userID, text))
	}

	f.run(updates...)

	var order []string
	for _, call := range f.replier.recorded() {
		order = append(order, call.text)
	}
	if got := strings.Join(order, ""); got != "12345678" {
		t.Errorf("processing order = %s", got)
	}
}

func TestCloseWaitsForTurnInProgress(t *testing.T) {
	f := newFixture()
	f.replier.started = make(chan struct{}, 1)
	f.replier.release = make(chan struct{})
	s := New(f.opts)
	s.Handle(textFrom(userID, "slow"))
	<-f.replier.started

	go func() {
		time.Sleep(30 * time.Millisecond)
		close(f.replier.release)
	}()
	s.Close()

	expectOneReply(t, f.sender.messages(), "echo: slow")
}

func TestCloseRefusesQueuedUpdatesInsteadOfRunningThem(t *testing.T) {
	f := newFixture()
	f.opts.Workers = 1
	f.replier.started = make(chan struct{}, 8)
	f.replier.release = make(chan struct{})
	s := New(f.opts)
	s.Handle(textFrom(userID, "in progress"))
	<-f.replier.started
	s.Handle(textFrom(userID, "queued 1"))
	s.Handle(textFrom(userID, "queued 2"))

	go func() {
		time.Sleep(30 * time.Millisecond)
		close(f.replier.release)
	}()
	s.Close()

	if calls := f.replier.recorded(); len(calls) != 1 {
		t.Errorf("model calls = %d, want only the turn that was in progress", len(calls))
	}
	answered, busy := 0, 0
	for _, msg := range f.sender.messages() {
		switch msg.text {
		case "echo: in progress":
			answered++
		case textBusy:
			busy++
		}
	}
	if answered != 1 || busy != 2 {
		t.Errorf("sent = %+v, want 1 answer and 2 busy replies", f.sender.messages())
	}
}

func TestHelpTextListsEveryCommand(t *testing.T) {
	help := HelpText()
	for _, command := range []string{"/start", "/help", "/reset", "/model", "/about"} {
		if !strings.Contains(help, command) {
			t.Errorf("help text does not mention %s", command)
		}
	}
}
