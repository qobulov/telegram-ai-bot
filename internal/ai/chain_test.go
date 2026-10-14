package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingServer answers with status, or with a reply when status is 0,
// and keeps every request body.
type recordingServer struct {
	mu     sync.Mutex
	status int
	bodies []string
}

func (s *recordingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, string(body))
	status := s.status
	s.mu.Unlock()
	if status != 0 {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"busy"}}`)
		return
	}
	_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"from backup"}}]}`)
}

func (s *recordingServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func chainOf(t *testing.T, primary, backup *recordingServer) *Chain {
	t.Helper()
	client := func(server *recordingServer) *Client {
		ts := httptest.NewServer(server)
		t.Cleanup(ts.Close)
		return NewClient(Options{BaseURL: ts.URL, APIKey: testAPIKey, Model: "m", MaxAttempts: 1, AttemptTimeout: time.Second})
	}
	return NewChain(client(primary), client(backup), nil)
}

func TestBackupAnswersWhenThePrimaryIsBusy(t *testing.T) {
	for _, status := range []int{429, 503} {
		primary, backup := &recordingServer{status: status}, &recordingServer{}
		msg, err := chainOf(t, primary, backup).Complete(context.Background(), userRequest())

		if err != nil || msg.Content != "from backup" {
			t.Errorf("status %d: msg = %+v, err = %v", status, msg, err)
		}
	}
}

func TestBackupIsNotAskedWhenThePrimaryAnswers(t *testing.T) {
	primary, backup := &recordingServer{}, &recordingServer{}
	_, _ = chainOf(t, primary, backup).Complete(context.Background(), userRequest())

	if len(backup.requests()) != 0 {
		t.Errorf("backup asked %d times", len(backup.requests()))
	}
}

func TestBadRequestDoesNotGoToTheBackup(t *testing.T) {
	primary, backup := &recordingServer{status: 400}, &recordingServer{}
	if _, err := chainOf(t, primary, backup).Complete(context.Background(), userRequest()); err == nil {
		t.Fatal("expected the 400")
	}
	if len(backup.requests()) != 0 {
		t.Error("a request the primary rejected was sent to the backup")
	}
}

func TestBackupGetsNoAudioAndNoGeminiSignatures(t *testing.T) {
	primary, backup := &recordingServer{status: 503}, &recordingServer{}
	req := Request{Messages: []Message{
		{Role: RoleUser, Content: `<voice message> "salom"`, Parts: []ContentPart{AudioPart("mp3", []byte("x")), ImagePart("image/png", []byte("y"))}},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Type: "function", Function: FunctionCall{Name: "f", Arguments: "{}"}, ExtraContent: json.RawMessage(`{"google":{"thought_signature":"abc"}}`)}}},
		{Role: RoleTool, ToolCallID: "1", Content: "ok"},
	}}

	if _, err := chainOf(t, primary, backup).Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	sent := backup.requests()[0]
	if strings.Contains(sent, "input_audio") || strings.Contains(sent, "thought_signature") {
		t.Errorf("backup request = %s", sent)
	}
	if !strings.Contains(sent, "image_url") || !strings.Contains(sent, "salom") {
		t.Errorf("backup request lost the picture or the transcript: %s", sent)
	}
	if !strings.Contains(string(primary.requests()[0]), "thought_signature") {
		t.Error("the primary request was changed too")
	}
}

func TestATurnThatMovedToTheBackupStaysThere(t *testing.T) {
	primary, backup := &recordingServer{}, &recordingServer{}
	// The backup made this tool call: it has no Gemini signature.
	req := Request{Messages: []Message{
		{Role: RoleUser, Content: "what time is it"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "fc_1", Type: "function", Function: FunctionCall{Name: "get_current_time", Arguments: "{}"}}}},
		{Role: RoleTool, ToolCallID: "fc_1", Content: "18:00"},
	}}

	if _, err := chainOf(t, primary, backup).Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(primary.requests()) != 0 || len(backup.requests()) != 1 {
		t.Errorf("primary asked %d, backup %d; Gemini cannot continue another model's tool calls", len(primary.requests()), len(backup.requests()))
	}
}

func TestEarlierTurnsDoNotDecideTheProvider(t *testing.T) {
	primary, backup := &recordingServer{}, &recordingServer{}
	req := Request{Messages: []Message{
		{Role: RoleUser, Content: "earlier"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "fc_1", Type: "function", Function: FunctionCall{Name: "f", Arguments: "{}"}}}},
		{Role: RoleTool, ToolCallID: "fc_1", Content: "ok"},
		{Role: RoleAssistant, Content: "done"},
		{Role: RoleUser, Content: "now"},
	}}

	_, _ = chainOf(t, primary, backup).Complete(context.Background(), req)
	if len(primary.requests()) != 1 {
		t.Error("a new turn should start with the primary")
	}
}

func TestPrimaryLeavesTimeForTheBackup(t *testing.T) {
	primary, backup := &recordingServer{status: 503}, &recordingServer{}
	chain := chainOf(t, primary, backup)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	msg, err := chain.Complete(ctx, userRequest())
	if err != nil || msg.Content != "from backup" {
		t.Errorf("msg = %+v, err = %v", msg, err)
	}
}
