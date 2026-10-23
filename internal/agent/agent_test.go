package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
)

const testUser = int64(42)

var testScope = storage.Scope{ChatID: testUser, UserID: testUser}

func request(updateID int64, text string) Request {
	return Request{Scope: testScope, UserID: testUser, UpdateID: updateID, Text: text}
}

// memoryStore is an in-memory Store keyed by conversation scope.
type memoryStore struct {
	history   map[storage.Scope][]ai.Message
	processed map[int64]bool
	turns     []storage.Turn
	saveErr   error
	saves     int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{history: map[storage.Scope][]ai.Message{}, processed: map[int64]bool{}}
}

func (s *memoryStore) History(_ context.Context, scope storage.Scope, limit int) ([]ai.Message, error) {
	all := s.history[scope]
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return append([]ai.Message(nil), all...), nil
}

func (s *memoryStore) SaveTurn(_ context.Context, turn storage.Turn) (int64, error) {
	if s.saveErr != nil {
		return 0, s.saveErr
	}
	s.saves++
	s.turns = append(s.turns, turn)
	s.processed[turn.UpdateID] = true
	s.history[turn.Scope] = append(s.history[turn.Scope], turn.Messages...)
	return int64(9000 + s.saves), nil
}

func (s *memoryStore) UpdateProcessed(_ context.Context, updateID int64) (bool, error) {
	return s.processed[updateID], nil
}

// scriptedModel returns its replies in order; the last one repeats.
type scriptedModel struct {
	replies  []ai.Message
	err      error
	requests []ai.Request
}

func (m *scriptedModel) Complete(_ context.Context, req ai.Request) (ai.Message, error) {
	m.requests = append(m.requests, req)
	if m.err != nil {
		return ai.Message{}, m.err
	}
	reply := m.replies[0]
	if len(m.replies) > 1 {
		m.replies = m.replies[1:]
	}
	return reply, nil
}

type toolExecution struct {
	callerID   int64
	name, args string
}

type fakeTools struct {
	executions []toolExecution
}

func (f *fakeTools) Definitions(context.Context, int64) []ai.ToolDefinition {
	return []ai.ToolDefinition{{Type: "function", Function: ai.FunctionDefinition{Name: "calculator"}}}
}

func (f *fakeTools) Execute(_ context.Context, callerID int64, name, args string) string {
	f.executions = append(f.executions, toolExecution{callerID: callerID, name: name, args: args})
	return fmt.Sprintf(`{"ok":true,"result":"ran %s"}`, name)
}

func text(content string) ai.Message {
	return ai.Message{Role: ai.RoleAssistant, Content: content}
}

func toolCall(ids ...string) ai.Message {
	msg := ai.Message{Role: ai.RoleAssistant}
	for _, id := range ids {
		msg.ToolCalls = append(msg.ToolCalls, ai.ToolCall{
			ID:       id,
			Type:     "function",
			Function: ai.FunctionCall{Name: "calculator", Arguments: `{"expression":"1+1"}`},
		})
	}
	return msg
}

type fixture struct {
	agent *Agent
	store *memoryStore
	model *scriptedModel
	tools *fakeTools
}

func newFixture(replies ...ai.Message) fixture {
	f := fixture{store: newMemoryStore(), model: &scriptedModel{replies: replies}, tools: &fakeTools{}}
	f.agent = New(Options{
		Store:        f.store,
		Model:        f.model,
		Tools:        f.tools,
		Persona:      "be funny",
		GroupNote:    "you are in a group",
		MaxHistory:   20,
		MaxToolCalls: 2,
	})
	return f
}

func roles(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, msg := range msgs {
		out[i] = msg.Role
	}
	return out
}

func TestReplyAnswersAndSavesTurn(t *testing.T) {
	f := newFixture(text("Salom!"))
	f.store.history[testScope] = []ai.Message{{Role: ai.RoleUser, Content: "earlier"}, text("earlier answer")}

	answer, err := f.agent.Reply(context.Background(), request(1, "hi"))
	if err != nil || answer.Text != "Salom!" {
		t.Fatalf("answer = %q, err = %v", answer.Text, err)
	}

	sent := f.model.requests[0]
	wantRoles := []string{ai.RoleSystem, ai.RoleUser, ai.RoleAssistant, ai.RoleUser}
	if !reflect.DeepEqual(roles(sent.Messages), wantRoles) {
		t.Errorf("request roles = %v, want %v", roles(sent.Messages), wantRoles)
	}
	if sent.Messages[0].Content != "be funny" || sent.Messages[3].Content != "hi" {
		t.Errorf("request messages = %+v", sent.Messages)
	}
	if len(sent.Tools) != 1 || sent.ForbidTools {
		t.Errorf("request tools = %+v, forbid = %v", sent.Tools, sent.ForbidTools)
	}

	saved := f.store.history[testScope][2:]
	if !reflect.DeepEqual(roles(saved), []string{ai.RoleUser, ai.RoleAssistant}) || saved[1].Content != "Salom!" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestReplyRunsToolAndFeedsResultBack(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("2"))

	answer, err := f.agent.Reply(context.Background(), request(1, "1+1?"))
	if err != nil || answer.Text != "2" {
		t.Fatalf("answer = %q, err = %v", answer.Text, err)
	}

	if len(f.tools.executions) != 1 {
		t.Fatalf("executions = %+v", f.tools.executions)
	}
	if got := f.tools.executions[0]; got.callerID != testUser || got.name != "calculator" || got.args != `{"expression":"1+1"}` {
		t.Errorf("execution = %+v", got)
	}

	second := f.model.requests[1].Messages
	last := second[len(second)-1]
	if last.Role != ai.RoleTool || last.ToolCallID != "call_1" || last.Content != `{"ok":true,"result":"ran calculator"}` {
		t.Errorf("tool message = %+v", last)
	}
	if before := second[len(second)-2]; len(before.ToolCalls) != 1 {
		t.Errorf("assistant tool-call message missing before the result: %+v", before)
	}

	wantSaved := []string{ai.RoleUser, ai.RoleAssistant, ai.RoleTool, ai.RoleAssistant}
	if got := roles(f.store.history[testScope]); !reflect.DeepEqual(got, wantSaved) {
		t.Errorf("saved roles = %v, want %v", got, wantSaved)
	}
}

func TestReplyStopsToolLoopAtLimit(t *testing.T) {
	f := newFixture(toolCall("a"), toolCall("b"), toolCall("c"), text("done"))

	answer, err := f.agent.Reply(context.Background(), request(1, "loop"))
	if err != nil || answer.Text != "done" {
		t.Fatalf("answer = %q, err = %v", answer.Text, err)
	}
	if len(f.tools.executions) != 2 {
		t.Errorf("executions = %d, want the limit of 2", len(f.tools.executions))
	}

	forbidden := []bool{}
	for _, req := range f.model.requests {
		forbidden = append(forbidden, req.ForbidTools)
	}
	if want := []bool{false, false, true, true}; !reflect.DeepEqual(forbidden, want) {
		t.Errorf("ForbidTools per request = %v, want %v", forbidden, want)
	}

	saved := f.store.history[testScope]
	refused := saved[len(saved)-2]
	if refused.Role != ai.RoleTool || refused.ToolCallID != "c" || !contains(refused.Content, "tool_limit") {
		t.Errorf("over-limit call result = %+v", refused)
	}
}

func TestReplyCountsParallelCallsAgainstLimit(t *testing.T) {
	f := newFixture(toolCall("a", "b", "c"), text("done"))

	if _, err := f.agent.Reply(context.Background(), request(1, "many")); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if len(f.tools.executions) != 2 {
		t.Errorf("executions = %d, want 2", len(f.tools.executions))
	}
	wantSaved := []string{ai.RoleUser, ai.RoleAssistant, ai.RoleTool, ai.RoleTool, ai.RoleTool, ai.RoleAssistant}
	if got := roles(f.store.history[testScope]); !reflect.DeepEqual(got, wantSaved) {
		t.Errorf("saved roles = %v, want %v (every call needs a result)", got, wantSaved)
	}
}

func TestReplyGivesUpWhenModelNeverAnswers(t *testing.T) {
	f := newFixture(toolCall("x"))

	_, err := f.agent.Reply(context.Background(), request(1, "loop forever"))
	if !errors.Is(err, ErrToolLoop) {
		t.Fatalf("err = %v, want ErrToolLoop", err)
	}
	if len(f.model.requests) != 4 {
		t.Errorf("model requests = %d, want MaxToolCalls+2 = 4", len(f.model.requests))
	}
	if f.store.saves != 0 {
		t.Error("a failed turn was saved")
	}
}

func TestReplySavesNothingWhenModelFails(t *testing.T) {
	f := newFixture()
	f.model.err = errors.New("provider down")

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); err == nil {
		t.Fatal("expected error")
	}
	if f.store.saves != 0 {
		t.Error("a failed turn was saved")
	}
}

func TestReplyRejectsEmptyAnswer(t *testing.T) {
	for _, content := range []string{"", "  \n"} {
		f := newFixture(text(content))

		_, err := f.agent.Reply(context.Background(), request(1, "hi"))
		if !errors.Is(err, ai.ErrEmptyResponse) {
			t.Errorf("content %q: err = %v, want ErrEmptyResponse", content, err)
		}
		if f.store.saves != 0 {
			t.Errorf("content %q: an empty turn was saved", content)
		}
	}
}

func TestReplySkipsProcessedUpdate(t *testing.T) {
	f := newFixture(text("first"))
	f.store.processed[7] = true

	_, err := f.agent.Reply(context.Background(), request(7, "again"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
	if len(f.model.requests) != 0 {
		t.Error("the model was called for a duplicate update")
	}
}

func TestReplyMapsDuplicateSave(t *testing.T) {
	f := newFixture(text("answer"))
	f.store.saveErr = storage.ErrDuplicateUpdate

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestReplyFillsMissingToolCallIDs(t *testing.T) {
	f := newFixture(toolCall(""), text("ok"))

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	saved := f.store.history[testScope]
	callID := saved[1].ToolCalls[0].ID
	if callID == "" || saved[2].ToolCallID != callID {
		t.Errorf("call id = %q, result id = %q; both must match and be non-empty", callID, saved[2].ToolCallID)
	}
}

func TestReplySendsOnlySanitizedHistory(t *testing.T) {
	f := newFixture(text("ok"))
	f.store.history[testScope] = []ai.Message{
		{Role: ai.RoleTool, ToolCallID: "lost", Content: "orphan"},
		text("answer without question"),
		{Role: ai.RoleUser, Content: "kept"},
	}

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	want := []string{ai.RoleSystem, ai.RoleUser, ai.RoleUser}
	if got := roles(f.model.requests[0].Messages); !reflect.DeepEqual(got, want) {
		t.Errorf("request roles = %v, want %v", got, want)
	}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestGroupTurnUsesSharedScopeAndTheSendersPermissions(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("group answer"))
	groupScope := storage.Scope{ChatID: -100500} // shared: no user
	f.store.history[testScope] = []ai.Message{{Role: ai.RoleUser, Content: "private secret"}, text("private answer")}

	answer, err := f.agent.Reply(context.Background(), Request{
		Scope:            groupScope,
		UserID:           testUser,
		AuthorName:       "Bek",
		UpdateID:         5,
		MessageID:        30,
		ReplyToMessageID: 29,
		Text:             "Bek: hello group",
		Group:            true,
	})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if answer.Text != "group answer" || answer.ID != 9001 {
		t.Errorf("answer = %+v, want the text and the stored row ID 9001", answer)
	}

	sent := f.model.requests[0].Messages
	if sent[0].Content != "be funny\n\nyou are in a group" {
		t.Errorf("system prompt = %q", sent[0].Content)
	}
	for _, msg := range sent {
		if strings.Contains(msg.Content, "private") {
			t.Errorf("private history leaked into a group turn: %q", msg.Content)
		}
	}
	if got := f.tools.executions[0].callerID; got != testUser {
		t.Errorf("tool caller = %d, want the sender %d", got, testUser)
	}

	saved := f.store.turns[0]
	if saved.Scope != groupScope || saved.UpdateID != 5 || saved.MessageID != 30 || saved.ReplyToMessageID != 29 {
		t.Errorf("saved turn = %+v", saved)
	}
	if saved.AuthorID != testUser || saved.AuthorName != "Bek" {
		t.Errorf("saved author = %d %q", saved.AuthorID, saved.AuthorName)
	}
	if len(f.store.history[testScope]) != 2 {
		t.Error("a group turn was written into the private history")
	}
}

func TestPrivateTurnUsesThePersonaAlone(t *testing.T) {
	f := newFixture(text("ok"))

	if _, err := f.agent.Reply(context.Background(), request(1, "hi")); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if got := f.model.requests[0].Messages[0].Content; got != "be funny" {
		t.Errorf("system prompt = %q", got)
	}
}

func TestContextIsSentForOneTurnAndNeverSaved(t *testing.T) {
	f := newFixture(text("first answer"), text("second answer"))
	req := request(1, "Bek: who is right?")
	req.Context = "[Recent messages]\nAnn: Go is interpreted\n[End]"

	if _, err := f.agent.Reply(context.Background(), req); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	sent := f.model.requests[0].Messages
	last := sent[len(sent)-1]
	if last.Role != ai.RoleUser || !strings.HasPrefix(last.Content, req.Context) || !strings.HasSuffix(last.Content, "Bek: who is right?") {
		t.Errorf("user message sent to the model = %q", last.Content)
	}
	for _, msg := range f.store.turns[0].Messages {
		if strings.Contains(msg.Content, "Go is interpreted") {
			t.Errorf("the context block was saved: %q", msg.Content)
		}
	}
	if got := f.store.turns[0].Messages[0].Content; got != "Bek: who is right?" {
		t.Errorf("saved user message = %q", got)
	}

	if _, err := f.agent.Reply(context.Background(), request(2, "Bek: and now?")); err != nil {
		t.Fatalf("second Reply: %v", err)
	}
	for _, msg := range f.model.requests[1].Messages {
		if strings.Contains(msg.Content, "Go is interpreted") {
			t.Errorf("the earlier context block came back in a later turn: %q", msg.Content)
		}
	}
}

func TestContextStaysInFrontThroughToolCalls(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("done"))
	req := request(1, "what time is it?")
	req.Context = "[Recent messages]"

	if _, err := f.agent.Reply(context.Background(), req); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	second := f.model.requests[1].Messages
	if question := second[1]; !strings.HasPrefix(question.Content, "[Recent messages]") {
		t.Errorf("the context was dropped from the follow-up request: %q", question.Content)
	}
}

func TestSilentAnswerSendsAndSavesNothing(t *testing.T) {
	for _, content := range []string{"[silent]", "[Silent]", " [silent]\n", "[SILENT]"} {
		f := newFixture(text(content))

		_, err := f.agent.Reply(context.Background(), request(1, "Mira is my sister's name"))
		if !errors.Is(err, ErrSilent) {
			t.Errorf("answer %q: err = %v, want ErrSilent", content, err)
		}
		if f.store.saves != 0 {
			t.Errorf("answer %q: a silent turn was saved", content)
		}
	}
}

// The marker leaked into groups when the model wrapped it in a sentence, so
// wherever it appears the whole answer is withheld.
func TestSilentMarkerAnywhereSilencesTheAnswer(t *testing.T) {
	for _, content := range []string{
		"Yana bitta gap bo'lsa, [silent].",
		"(SILENT)",
		"ok [Silent]",
		"I could answer [silent] but where is the fun in that",
		"(silent) — they are not talking to me",
	} {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "hm"))
		if !errors.Is(err, ErrSilent) {
			t.Errorf("answer %q: got %+v, err = %v, want ErrSilent", content, answer, err)
		}
		if f.store.saves != 0 {
			t.Errorf("answer %q: a silent turn was saved", content)
		}
	}
}

// In production the model once sent its own reasoning, a stray character
// and "thought" in front, into a group. Such an answer is never sent.
func TestLeakedThinkingIsNotSent(t *testing.T) {
	for _, content := range []string{
		"쳤thought He replied to his own message, so explain it.Username bilan mute qila olmayman.",
		"thought\nThe user asks about the weather; call get_weather.",
		" *thought The member is joking.",
	} {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "hm"))
		if !errors.Is(err, ErrSilent) || !errors.Is(err, ErrLeakedThinking) {
			t.Errorf("answer %q: got %+v, err = %v, want ErrLeakedThinking", content, answer, err)
		}
		if f.store.saves != 0 {
			t.Errorf("answer %q: the leaked thinking was saved", content)
		}
	}
	for _, content := range []string{"Thought so 😏", "thought so, ha", "Thoughtful of you"} {
		f := newFixture(text(content))
		if answer, err := f.agent.Reply(context.Background(), request(1, "hm")); err != nil || answer.Text != content {
			t.Errorf("answer %q: got %+v, err = %v; ordinary words are an answer", content, answer, err)
		}
	}
}

func TestSilentAsAnOrdinaryWordIsAnAnswer(t *testing.T) {
	f := newFixture(text("Silent night, holy night"))

	if answer, err := f.agent.Reply(context.Background(), request(1, "sing")); err != nil || answer.Text != "Silent night, holy night" {
		t.Errorf("answer = %+v, err = %v; only the marker silences", answer, err)
	}
}

func TestReactAnswerIsSavedAsReaction(t *testing.T) {
	cases := map[string]string{
		"[react 🔥]":    "🔥",
		"[React 🔥]":    "🔥",
		" [react  🔥 ]": "🔥",
		"[react\n🔥]":   "🔥",
		"*[react 🔥]*":  "🔥",
		"[react 🔥].":   "🔥",
		// Models often add the U+FE0F variation selector, which Telegram's
		// list does not have.
		"[react ❤\uFE0F]":        "❤",
		"[react ❤\uFE0F\u200d🔥]": "❤\u200d🔥",
		"[react ⚡\uFE0F]":        "⚡",
		// The history shows past reactions in this form, and models copy it.
		"<reacted 👍>": "👍",
		// Models change the brackets and the word, as they do with silence.
		"(react 🔥)":    "🔥",
		"(React: 🔥)":   "🔥",
		"[reaction 🔥]": "🔥",
		"(reaction 👍)": "👍",
		"<reaction 👍>": "👍",
	}
	for content, want := range cases {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa"))
		if err != nil {
			t.Errorf("answer %q: err = %v", content, err)
			continue
		}
		if answer.Reaction != want || answer.Text != "" || answer.ID != 9001 {
			t.Errorf("answer %q: got %+v, want reaction %q, no text, the stored row's ID", content, answer, want)
		}
		saved := f.store.history[testScope]
		if last := saved[len(saved)-1]; last.Role != ai.RoleAssistant || last.Content != "<reacted "+want+">" {
			t.Errorf("answer %q: saved last message = %+v, want <reacted %s>", content, last, want)
		}
	}
}

// [silent] leaked into groups inside sentences; [react X] and the
// <reacted X> of the history do too, so a marker among words is dropped and
// the words are sent.
func TestReactMarkerAmongWordsIsDropped(t *testing.T) {
	cases := map[string]string{
		"Zo'r! [react 🔥]":                    "Zo'r!",
		"[react 😁] xaxa":                     "xaxa",
		"Haha <reacted 🤣>":                   "Haha",
		"Ha, <Reacted 👍> qoyil":              "Ha, qoyil",
		"Birinchi qator [react 🔥]\nIkkinchi": "Birinchi qator\nIkkinchi",
		"[reacted 🔥] ok":                     "ok",
		"Zo'r! (react 🔥)":                    "Zo'r!",
		"Zo'r [reaction 🔥]":                  "Zo'r",
		"(reacted 😁) xaxa":                   "xaxa",
	}
	for content, want := range cases {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa zo'r"))
		if err != nil || answer.Text != want || answer.Reaction != "" {
			t.Errorf("answer %q: got %+v, err = %v; want the text %q", content, answer, err, want)
			continue
		}
		saved := f.store.history[testScope]
		if last := saved[len(saved)-1]; last.Content != want {
			t.Errorf("answer %q: saved %q, want %q", content, last.Content, want)
		}
	}
}

// Round brackets are common in prose, so only a round marker with an emoji
// and no words in it is taken for one.
func TestRoundBracketsAroundTheWordReactAreText(t *testing.T) {
	for _, content := range []string{
		"Men frontendda (React) ishlayman",
		"Kutubxona (react.js) yaxshi",
		"Uni (react native) bilan yozganman",
		"(React 18) chiqdi",
	} {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "qaysi?"))
		if err != nil || answer.Text != content || answer.Reaction != "" {
			t.Errorf("answer %q: got %+v, err = %v; want it sent as written", content, answer, err)
		}
	}
}

// Maria's history is full of 😂, which Telegram does not take as a
// reaction; nor 🙂 or a skin tone. The nearest one it takes is set instead of
// nothing.
func TestReactWithANearMissEmojiUsesTheNearestReaction(t *testing.T) {
	cases := map[string]string{
		"[react 😂]":  "🤣",
		"[react 😆]":  "🤣",
		"[react 😅]":  "😁",
		"[react 😄]":  "😁",
		"[react 🙂]":  "👍",
		"[react 😊]":  "👍",
		"[react 💖]":  "❤",
		"[react 👍🏻]": "👍",
		"[react 👏🏽]": "👏",
	}
	for content, want := range cases {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa"))
		if err != nil || answer.Reaction != want {
			t.Errorf("answer %q: got %+v, err = %v; want the reaction %s", content, answer, err, want)
		}
	}
}

func TestReactWithUnknownEmojiIsSilent(t *testing.T) {
	for _, content := range []string{"[react 🥲]", "[react]", "[react 🔥🔥]", "[react fire]", "[react 🔥 👍]"} {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa"))
		if !errors.Is(err, ErrSilent) {
			t.Errorf("answer %q: got %+v, err = %v, want ErrSilent", content, answer, err)
		}
		if f.store.saves != 0 {
			t.Errorf("answer %q: a turn Telegram cannot show was saved", content)
		}
	}
}

func TestSilentMarkerWithDecoration(t *testing.T) {
	for _, content := range []string{"[silent].", "*[silent]*", "\"[silent]\"", "[silent]!"} {
		f := newFixture(text(content))

		if _, err := f.agent.Reply(context.Background(), request(1, "hm")); !errors.Is(err, ErrSilent) {
			t.Errorf("answer %q: err = %v, want ErrSilent", content, err)
		}
	}
}

func TestAttachmentsAreSentWithTheQuestionButNeverSaved(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("nice cat"), text("still nice"))
	photo := ai.ImagePart("image/jpeg", []byte{1, 2, 3})
	req := request(1, "Bek: <photo> look")
	req.Attachments = []ai.ContentPart{photo}

	if _, err := f.agent.Reply(context.Background(), req); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	for i, sent := range f.model.requests {
		question := sent.Messages[1]
		if len(question.Parts) != 1 || question.Parts[0].ImageURL == nil || question.Content != "Bek: <photo> look" {
			t.Errorf("request %d: question = %+v, want the text with the photo attached", i, question)
		}
	}
	for _, msg := range f.store.turns[0].Messages {
		if len(msg.Parts) != 0 {
			t.Errorf("a saved message carries media: %+v", msg)
		}
	}

	if _, err := f.agent.Reply(context.Background(), request(2, "Bek: and now?")); err != nil {
		t.Fatalf("second Reply: %v", err)
	}
	for _, msg := range f.model.requests[2].Messages {
		if len(msg.Parts) != 0 {
			t.Errorf("the earlier photo came back in a later turn: %+v", msg)
		}
	}
}

// A sticker marker alone asks for a sticker of its category. With words it
// is dropped, as the owners asked for one or the other: the words are sent.
// It is never sent as text.
func TestStickerMarkerIsTakenOut(t *testing.T) {
	cases := map[string]struct{ text, sticker, saved string }{
		"Voy, yana maqtanish 😂 [sticker sarcasm]": {"Voy, yana maqtanish 😂", "", "Voy, yana maqtanish 😂"},
		"[Sticker Funny]":                       {"", "funny", "<sticker funny>"},
		"<sticker sad>":                         {"", "sad", "<sticker sad>"},
		"Tabriklayman! [sticker celebration]\n": {"Tabriklayman!", "", "Tabriklayman!"},
		"Oddiy javob":                           {"Oddiy javob", "", "Oddiy javob"},
	}
	for content, want := range cases {
		f := newFixture(text(content))

		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa"))
		if err != nil || answer.Text != want.text || answer.Sticker != want.sticker {
			t.Errorf("answer %q: got %+v, err = %v; want text %q and sticker %q", content, answer, err, want.text, want.sticker)
			continue
		}
		saved := f.store.history[testScope]
		if last := saved[len(saved)-1]; last.Content != want.saved {
			t.Errorf("answer %q: saved %q, want %q", content, last.Content, want.saved)
		}
	}
}

// "[gif X]" asks for a GIF the way "[sticker X]" asks for a sticker.
func TestGIFMarkerIsTakenOut(t *testing.T) {
	cases := map[string]struct{ text, gif, saved string }{
		"Yana shu ahvol [gif facepalm]": {"Yana shu ahvol", "", "Yana shu ahvol"},
		"[GIF celebration]":             {"", "celebration", "<gif celebration>"},
		"[gif PUBG  winner]":            {"", "pubg winner", "<gif pubg winner>"},
	}
	for content, want := range cases {
		f := newFixture(text(content))
		answer, err := f.agent.Reply(context.Background(), request(1, "xaxa"))
		if err != nil || answer.Text != want.text || answer.GIF != want.gif || answer.Sticker != "" {
			t.Errorf("answer %q: got %+v, err = %v; want text %q and GIF %q", content, answer, err, want.text, want.gif)
			continue
		}
		saved := f.store.history[testScope]
		if last := saved[len(saved)-1]; last.Content != want.saved {
			t.Errorf("answer %q: saved %q, want %q", content, last.Content, want.saved)
		}
	}
}

// A tool call written as text is never posted: the model is asked once more,
// told to call the tool, and when it writes one again nothing is sent.
func TestToolCallWrittenAsTextIsNotSent(t *testing.T) {
	f := newFixture(text("[music_search query:Chammak chalo]"), toolCall("c1"), text("Mana."))

	answer, err := f.agent.Reply(context.Background(), request(1, "Chammak chalo mari"))
	if err != nil || answer.Text != "Mana." {
		t.Fatalf("answer = %+v, %v; want the tool run and then the words", answer, err)
	}
	if len(f.tools.executions) != 1 {
		t.Errorf("executions = %d, want 1", len(f.tools.executions))
	}
	if second := f.model.requests[1].Messages[0].Content; !strings.Contains(second, toolAsTextNote) {
		t.Errorf("the second request does not tell the model to call the tool: %q", second)
	}
	if first := f.model.requests[0].Messages[0].Content; strings.Contains(first, toolAsTextNote) {
		t.Errorf("the first request already carries the note")
	}

	for _, content := range []string{"[generate_image prompt:Hirara portrait]", "(calculator expression: 1+1)"} {
		f := newFixture(text(content))
		answer, err := f.agent.Reply(context.Background(), request(1, "hm"))
		if !errors.Is(err, ErrToolAsText) || !errors.Is(err, ErrSilent) {
			t.Errorf("answer %q: got %+v, err = %v, want ErrToolAsText", content, answer, err)
		}
		if f.store.saves != 0 {
			t.Errorf("answer %q: the tool call text was saved", content)
		}
	}
	for _, content := range []string{"[sticker funny]", "(ha: rost)", "Saber: 2-1-ulti", "[react 🤣]"} {
		f := newFixture(text(content))
		if _, err := f.agent.Reply(context.Background(), request(1, "hm")); errors.Is(err, ErrToolAsText) {
			t.Errorf("answer %q was taken for a tool call", content)
		}
	}
}

// "[to them]" opening an answer sends it to the member the asker replied
// to; it is never posted or saved, and anywhere else it is no marker.
func TestToThemMarkerAddressesTheRepliedMember(t *testing.T) {
	for content, want := range map[string]Answer{
		"[to them] Miyang kichkina emas.": {Text: "Miyang kichkina emas.", ToReplied: true},
		"(To Them)Salom":                  {Text: "Salom", ToReplied: true},
		"Salom [to them]":                 {Text: "Salom [to them]"},
		"[to Chaizooo @naniwo_desu]\n손이 안 닿으니까 걱정 마세요.": {Text: "손이 안 닿으니까 걱정 마세요.", ToReplied: true, To: "Chaizooo @naniwo_desu"},
		"[to James] [gif funny]":   {GIF: "funny", ToReplied: true, To: "James"},
		"(to be honest) it's fine": {Text: "(to be honest) it's fine"},
		"[to them] [react 🤣]":      {Reaction: "🤣"},
	} {
		f := newFixture(text(content))
		answer, err := f.agent.Reply(context.Background(), request(1, "Mari, bunga bir nima de"))
		answer.ID = 0
		if err != nil || answer != want {
			t.Errorf("%q: got %+v, %v; want %+v", content, answer, err, want)
		}
		for _, m := range f.store.history[testScope] {
			if want.ToReplied && strings.Contains(strings.ToLower(m.Content), "to them") {
				t.Errorf("%q: saved %q with the marker", content, m.Content)
			}
		}
	}
}

// A fixed answer is saved as the turn, without the model, and answers a
// redelivered update once.
func TestFixedAnswerIsSavedWithoutTheModel(t *testing.T) {
	f := newFixture(text("never asked"))
	req := request(7, "salom")
	req.Fixed = &Answer{Text: "Salom! Nima gaplar?"}

	answer, err := f.agent.Reply(context.Background(), req)
	if err != nil || answer.Text != "Salom! Nima gaplar?" || answer.ID == 0 {
		t.Fatalf("answer = %+v, %v", answer, err)
	}
	if len(f.model.requests) != 0 {
		t.Errorf("model asked %d times", len(f.model.requests))
	}
	if h := f.store.history[testScope]; len(h) != 2 || h[1].Content != "Salom! Nima gaplar?" {
		t.Errorf("history = %+v", h)
	}
	if _, err := f.agent.Reply(context.Background(), req); !errors.Is(err, ErrDuplicate) {
		t.Errorf("redelivered: err = %v, want ErrDuplicate", err)
	}
}
