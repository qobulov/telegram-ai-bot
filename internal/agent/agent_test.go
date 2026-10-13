package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
)

const testUser = int64(42)

// memoryStore is an in-memory Store keyed by user ID.
type memoryStore struct {
	history   map[int64][]ai.Message
	processed map[int64]bool
	saveErr   error
	saves     int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{history: map[int64][]ai.Message{}, processed: map[int64]bool{}}
}

func (s *memoryStore) History(_ context.Context, userID int64, limit int) ([]ai.Message, error) {
	all := s.history[userID]
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return append([]ai.Message(nil), all...), nil
}

func (s *memoryStore) SaveTurn(_ context.Context, userID, updateID int64, msgs []ai.Message) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saves++
	s.processed[updateID] = true
	s.history[userID] = append(s.history[userID], msgs...)
	return nil
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

func (f *fakeTools) Definitions(int64) []ai.ToolDefinition {
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
		SystemPrompt: "be helpful",
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
	f.store.history[testUser] = []ai.Message{{Role: ai.RoleUser, Content: "earlier"}, text("earlier answer")}

	answer, err := f.agent.Reply(context.Background(), testUser, 1, "hi")
	if err != nil || answer != "Salom!" {
		t.Fatalf("answer = %q, err = %v", answer, err)
	}

	sent := f.model.requests[0]
	wantRoles := []string{ai.RoleSystem, ai.RoleUser, ai.RoleAssistant, ai.RoleUser}
	if !reflect.DeepEqual(roles(sent.Messages), wantRoles) {
		t.Errorf("request roles = %v, want %v", roles(sent.Messages), wantRoles)
	}
	if sent.Messages[0].Content != "be helpful" || sent.Messages[3].Content != "hi" {
		t.Errorf("request messages = %+v", sent.Messages)
	}
	if len(sent.Tools) != 1 || sent.ForbidTools {
		t.Errorf("request tools = %+v, forbid = %v", sent.Tools, sent.ForbidTools)
	}

	saved := f.store.history[testUser][2:]
	if !reflect.DeepEqual(roles(saved), []string{ai.RoleUser, ai.RoleAssistant}) || saved[1].Content != "Salom!" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestReplyRunsToolAndFeedsResultBack(t *testing.T) {
	f := newFixture(toolCall("call_1"), text("2"))

	answer, err := f.agent.Reply(context.Background(), testUser, 1, "1+1?")
	if err != nil || answer != "2" {
		t.Fatalf("answer = %q, err = %v", answer, err)
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
	if got := roles(f.store.history[testUser]); !reflect.DeepEqual(got, wantSaved) {
		t.Errorf("saved roles = %v, want %v", got, wantSaved)
	}
}

func TestReplyStopsToolLoopAtLimit(t *testing.T) {
	f := newFixture(toolCall("a"), toolCall("b"), toolCall("c"), text("done"))

	answer, err := f.agent.Reply(context.Background(), testUser, 1, "loop")
	if err != nil || answer != "done" {
		t.Fatalf("answer = %q, err = %v", answer, err)
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

	saved := f.store.history[testUser]
	refused := saved[len(saved)-2]
	if refused.Role != ai.RoleTool || refused.ToolCallID != "c" || !contains(refused.Content, "tool_limit") {
		t.Errorf("over-limit call result = %+v", refused)
	}
}

func TestReplyCountsParallelCallsAgainstLimit(t *testing.T) {
	f := newFixture(toolCall("a", "b", "c"), text("done"))

	if _, err := f.agent.Reply(context.Background(), testUser, 1, "many"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if len(f.tools.executions) != 2 {
		t.Errorf("executions = %d, want 2", len(f.tools.executions))
	}
	wantSaved := []string{ai.RoleUser, ai.RoleAssistant, ai.RoleTool, ai.RoleTool, ai.RoleTool, ai.RoleAssistant}
	if got := roles(f.store.history[testUser]); !reflect.DeepEqual(got, wantSaved) {
		t.Errorf("saved roles = %v, want %v (every call needs a result)", got, wantSaved)
	}
}

func TestReplyGivesUpWhenModelNeverAnswers(t *testing.T) {
	f := newFixture(toolCall("x"))

	_, err := f.agent.Reply(context.Background(), testUser, 1, "loop forever")
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

	if _, err := f.agent.Reply(context.Background(), testUser, 1, "hi"); err == nil {
		t.Fatal("expected error")
	}
	if f.store.saves != 0 {
		t.Error("a failed turn was saved")
	}
}

func TestReplyRejectsEmptyAnswer(t *testing.T) {
	for _, content := range []string{"", "  \n"} {
		f := newFixture(text(content))

		_, err := f.agent.Reply(context.Background(), testUser, 1, "hi")
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

	_, err := f.agent.Reply(context.Background(), testUser, 7, "again")
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

	if _, err := f.agent.Reply(context.Background(), testUser, 1, "hi"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestReplyFillsMissingToolCallIDs(t *testing.T) {
	f := newFixture(toolCall(""), text("ok"))

	if _, err := f.agent.Reply(context.Background(), testUser, 1, "hi"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	saved := f.store.history[testUser]
	callID := saved[1].ToolCalls[0].ID
	if callID == "" || saved[2].ToolCallID != callID {
		t.Errorf("call id = %q, result id = %q; both must match and be non-empty", callID, saved[2].ToolCallID)
	}
}

func TestReplySendsOnlySanitizedHistory(t *testing.T) {
	f := newFixture(text("ok"))
	f.store.history[testUser] = []ai.Message{
		{Role: ai.RoleTool, ToolCallID: "lost", Content: "orphan"},
		text("answer without question"),
		{Role: ai.RoleUser, Content: "kept"},
	}

	if _, err := f.agent.Reply(context.Background(), testUser, 1, "hi"); err != nil {
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
