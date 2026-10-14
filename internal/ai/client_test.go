package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAPIKey = "sk-or-test-secret"

const plainReply = `{"choices":[{"message":{"role":"assistant","content":"Salom!"}}]}`

// recorder is a fake OpenRouter that replies with a scripted sequence and
// remembers what it was sent.
type recorder struct {
	mu       sync.Mutex
	replies  []reply
	requests []map[string]any
	headers  []http.Header
}

type reply struct {
	status int
	body   string
	header map[string]string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var payload map[string]any
	body, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(body, &payload)
	r.requests = append(r.requests, payload)
	r.headers = append(r.headers, req.Header.Clone())

	next := r.replies[0]
	if len(r.replies) > 1 {
		r.replies = r.replies[1:]
	}
	for key, value := range next.header {
		w.Header().Set(key, value)
	}
	w.WriteHeader(next.status)
	_, _ = io.WriteString(w, next.body)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func newTestClient(t *testing.T, replies ...reply) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{replies: replies}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)
	client := NewClient(Options{
		BaseURL:   server.URL,
		APIKey:    testAPIKey,
		Model:     "test/model",
		MaxTokens: 256,
		BaseDelay: time.Millisecond,
	})
	return client, rec
}

func userRequest() Request {
	return Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
}

func toolRequest() Request {
	req := userRequest()
	req.Tools = []ToolDefinition{{
		Type: "function",
		Function: FunctionDefinition{
			Name:        "get_current_time",
			Description: "time",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}}
	return req
}

func TestCompleteSendsRequestAndDecodesReply(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 200, body: plainReply})

	msg, err := client.Complete(context.Background(), toolRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Role != RoleAssistant || msg.Content != "Salom!" {
		t.Errorf("msg = %+v", msg)
	}

	if got := rec.headers[0].Get("Authorization"); got != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q", got)
	}
	sent := rec.requests[0]
	if sent["model"] != "test/model" || sent["max_tokens"] != float64(256) {
		t.Errorf("request = %v", sent)
	}
	if tools, _ := sent["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools = %v", sent["tools"])
	}
	if _, present := sent["tool_choice"]; present {
		t.Errorf("tool_choice sent unexpectedly: %v", sent["tool_choice"])
	}
}

func TestCompleteDecodesToolCalls(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
		{"id":"call_1","type":"function","function":{"name":"calculator","arguments":"{\"expression\":\"1+1\"}"}}]}}]}`
	client, _ := newTestClient(t, reply{status: 200, body: body})

	msg, err := client.Complete(context.Background(), toolRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if msg.Content != "" || len(msg.ToolCalls) != 1 {
		t.Fatalf("msg = %+v", msg)
	}
	call := msg.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "calculator" || call.Function.Arguments != `{"expression":"1+1"}` {
		t.Errorf("call = %+v", call)
	}
}

func TestCompleteRetriesRateLimit(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 429, body: `{"error":{"message":"slow down"}}`, header: map[string]string{"Retry-After": "0"}},
		reply{status: 200, body: plainReply},
	)

	if _, err := client.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if rec.count() != 2 {
		t.Errorf("requests = %d, want 2", rec.count())
	}
}

func TestCompleteRetriesServerErrors(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 500, body: "boom"},
		reply{status: 502, body: "boom"},
		reply{status: 200, body: plainReply},
	)

	if _, err := client.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if rec.count() != 3 {
		t.Errorf("requests = %d, want 3", rec.count())
	}
}

func TestCompleteStopsAtRetryLimit(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 500, body: `{"error":{"message":"down"}}`})

	_, err := client.Complete(context.Background(), userRequest())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 500 || apiErr.Message != "down" {
		t.Fatalf("err = %v", err)
	}
	if rec.count() != 3 {
		t.Errorf("requests = %d, want 3", rec.count())
	}
}

func TestCompleteDoesNotRetryClientErrors(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 400, body: `{"error":{"message":"bad request"}}`})

	_, err := client.Complete(context.Background(), userRequest())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("err = %v", err)
	}
	if rec.count() != 1 {
		t.Errorf("requests = %d, want 1", rec.count())
	}
}

func TestCompleteHonoursRetryAfter(t *testing.T) {
	client, _ := newTestClient(t,
		reply{status: 429, body: "{}", header: map[string]string{"Retry-After": "1"}},
		reply{status: 200, body: plainReply},
	)

	start := time.Now()
	if _, err := client.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if waited := time.Since(start); waited < 900*time.Millisecond {
		t.Errorf("waited %v, want about 1s", waited)
	}
}

func TestCompleteReturnsErrorFromSuccessBody(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 200, body: `{"error":{"message":"provider failed","code":400}}`})

	_, err := client.Complete(context.Background(), userRequest())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "provider failed" {
		t.Fatalf("err = %v", err)
	}
	if rec.count() != 1 {
		t.Errorf("requests = %d, want 1", rec.count())
	}
}

func TestCompleteRejectsEmptyChoices(t *testing.T) {
	client, _ := newTestClient(t, reply{status: 200, body: `{"choices":[]}`})

	_, err := client.Complete(context.Background(), userRequest())
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
}

func TestCompleteRejectsMalformedJSON(t *testing.T) {
	client, _ := newTestClient(t, reply{status: 200, body: `<html>gateway</html>`})

	if _, err := client.Complete(context.Background(), userRequest()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompleteStopsWhenContextCancelled(t *testing.T) {
	client, _ := newTestClient(t,
		reply{status: 429, body: "{}", header: map[string]string{"Retry-After": "30"}},
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Complete(ctx, userRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %v, should stop at the deadline", time.Since(start))
	}
}

func TestCompleteFallsBackWhenToolsUnsupported(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 404, body: `{"error":{"message":"No endpoints found that support tool use."}}`},
		reply{status: 200, body: plainReply},
	)

	msg, err := client.Complete(context.Background(), toolRequest())
	if err != nil || msg.Content != "Salom!" {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
	if _, present := rec.requests[1]["tools"]; present {
		t.Error("retry still sent tools")
	}

	if _, err := client.Complete(context.Background(), toolRequest()); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if _, present := rec.requests[2]["tools"]; present {
		t.Error("later request sent tools after fallback")
	}
}

func TestUnrelatedToolErrorDoesNotDisableTools(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 400, body: `{"error":{"message":"maximum context length exceeded: 500 tokens of tool input"}}`},
		reply{status: 200, body: plainReply},
	)

	if _, err := client.Complete(context.Background(), toolRequest()); err == nil {
		t.Fatal("expected the context-length error to be returned")
	}
	if _, err := client.Complete(context.Background(), toolRequest()); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	last := rec.requests[len(rec.requests)-1]
	if _, present := last["tools"]; !present {
		t.Error("tools were disabled by an error that is not about tool support")
	}
}

func TestFailedFallbackDoesNotDisableTools(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 404, body: `{"error":{"message":"No endpoints found that support tool use."}}`},
		reply{status: 400, body: `{"error":{"message":"bad request"}}`},
		reply{status: 200, body: plainReply},
	)

	if _, err := client.Complete(context.Background(), toolRequest()); err == nil {
		t.Fatal("expected the fallback's error to be returned")
	}
	if _, err := client.Complete(context.Background(), toolRequest()); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if _, present := rec.requests[2]["tools"]; !present {
		t.Error("tools were disabled although the fallback request never succeeded")
	}
}

func TestCompleteForbidTools(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 200, body: plainReply})
	req := toolRequest()
	req.ForbidTools = true

	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if rec.requests[0]["tool_choice"] != "none" {
		t.Errorf("tool_choice = %v", rec.requests[0]["tool_choice"])
	}
}

func TestErrorsNeverContainAPIKey(t *testing.T) {
	client, _ := newTestClient(t, reply{status: 401, body: `{"error":{"message":"invalid key"}}`})
	_, apiErr := client.Complete(context.Background(), userRequest())

	unreachable := NewClient(Options{BaseURL: "http://127.0.0.1:1", APIKey: testAPIKey, Model: "m", MaxAttempts: 1})
	_, netErr := unreachable.Complete(context.Background(), userRequest())

	for _, err := range []error{apiErr, netErr} {
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), testAPIKey) {
			t.Errorf("error leaks API key: %v", err)
		}
	}
}

// Gemini's OpenAI-compatible endpoint wraps some errors in a JSON array.
func TestCompleteReadsErrorsWrappedInAnArray(t *testing.T) {
	client, _ := newTestClient(t, reply{status: 400, body: `[{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}]`})

	_, err := client.Complete(context.Background(), userRequest())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Message != "API key not valid" {
		t.Fatalf("err = %v", err)
	}
}

func TestMessageWithPartsIsSentAsContentArray(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 200, body: plainReply})
	req := Request{Messages: []Message{{
		Role:    RoleUser,
		Content: "Bek: what is this?",
		Parts: []ContentPart{
			ImagePart("image/jpeg", []byte{0xff, 0xd8, 0xff}),
			AudioPart("ogg", []byte("OggS")),
		},
	}}}

	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	messages, _ := rec.requests[0]["messages"].([]any)
	content, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(content) != 3 {
		t.Fatalf("content = %v, want text, image and audio parts", messages[0])
	}
	text := content[0].(map[string]any)
	image := content[1].(map[string]any)
	audio := content[2].(map[string]any)
	if text["type"] != "text" || text["text"] != "Bek: what is this?" {
		t.Errorf("text part = %v", text)
	}
	if image["type"] != "image_url" || image["image_url"].(map[string]any)["url"] != "data:image/jpeg;base64,/9j/" {
		t.Errorf("image part = %v", image)
	}
	inputAudio := audio["input_audio"].(map[string]any)
	if audio["type"] != "input_audio" || inputAudio["format"] != "ogg" || inputAudio["data"] != "T2dnUw==" {
		t.Errorf("audio part = %v", audio)
	}
}

func TestMessageWithoutPartsKeepsStringContent(t *testing.T) {
	encoded, err := json.Marshal(Message{Role: RoleUser, Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"role":"user","content":"hi"}` {
		t.Errorf("encoded = %s", encoded)
	}
}

// A provider under load sometimes accepts a request and then sits on it.
// One stuck attempt must not use up the whole turn: it is abandoned and the
// request is sent again.
func TestStuckAttemptIsAbandonedAndRetried(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			<-release // never answers the first attempt while the test runs
			return
		}
		_, _ = io.WriteString(w, plainReply)
	}))
	defer server.Close()
	defer close(release) // runs before Close, so the stuck handler can end
	client := NewClient(Options{
		BaseURL:        server.URL,
		APIKey:         testAPIKey,
		Model:          "m",
		BaseDelay:      time.Millisecond,
		AttemptTimeout: 100 * time.Millisecond,
	})

	start := time.Now()
	msg, err := client.Complete(context.Background(), userRequest())
	if err != nil || msg.Content != "Salom!" {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v; the stuck attempt should have been cut off after 100ms", elapsed)
	}
}

func TestIsOverloaded(t *testing.T) {
	for status, want := range map[int]bool{429: true, 503: true, 500: false, 400: false} {
		if got := IsOverloaded(&APIError{Status: status}); got != want {
			t.Errorf("IsOverloaded(%d) = %v, want %v", status, got, want)
		}
	}
	if IsOverloaded(errors.New("other")) {
		t.Error("a non-provider error was reported as overload")
	}
	if !IsOverloaded(fmt.Errorf("agent: %w", &APIError{Status: 503})) {
		t.Error("a wrapped overload error was not recognised")
	}
}

// modelServer answers per model: a model listed in down gets that status, the
// rest a normal reply. It records which model each request asked for.
type modelServer struct {
	mu     sync.Mutex
	down   map[string]int
	stuck  map[string]bool
	asked  []string
	cancel chan struct{}
}

func (m *modelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	m.asked = append(m.asked, req.Model)
	status, isDown := m.down[req.Model]
	isStuck := m.stuck[req.Model]
	m.mu.Unlock()
	switch {
	case isStuck:
		<-m.cancel
	case isDown:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"This model is currently experiencing high demand"}}`)
	default:
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"from `+req.Model+`"}}]}`)
	}
}

func fallbackClient(t *testing.T, server *modelServer) *Client {
	t.Helper()
	server.cancel = make(chan struct{})
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { close(server.cancel) })
	return NewClient(Options{
		BaseURL:        ts.URL,
		APIKey:         testAPIKey,
		Model:          "primary",
		FallbackModels: []string{"backup-1", "backup-2"},
		BaseDelay:      time.Millisecond,
		AttemptTimeout: 100 * time.Millisecond,
	})
}

func TestOverloadedModelFallsBackToTheNext(t *testing.T) {
	for _, status := range []int{503, 429} {
		server := &modelServer{down: map[string]int{"primary": status}}
		client := fallbackClient(t, server)

		msg, err := client.Complete(context.Background(), userRequest())

		if err != nil || msg.Content != "from backup-1" {
			t.Errorf("status %d: msg = %+v, err = %v", status, msg, err)
		}
		if got := strings.Join(server.asked, ","); got != "primary,backup-1" {
			t.Errorf("status %d: models asked = %s", status, got)
		}
	}
}

func TestStuckModelFallsBackToTheNext(t *testing.T) {
	server := &modelServer{stuck: map[string]bool{"primary": true}}
	client := fallbackClient(t, server)

	msg, err := client.Complete(context.Background(), userRequest())

	if err != nil || msg.Content != "from backup-1" {
		t.Errorf("msg = %+v, err = %v", msg, err)
	}
}

func TestEveryModelDownReturnsTheLastError(t *testing.T) {
	server := &modelServer{down: map[string]int{"primary": 503, "backup-1": 503, "backup-2": 503}}
	client := fallbackClient(t, server)

	_, err := client.Complete(context.Background(), userRequest())

	if !IsOverloaded(err) {
		t.Errorf("err = %v, want the overload error", err)
	}
	if got := strings.Join(server.asked, ","); got != "primary,backup-1,backup-2" {
		t.Errorf("models asked = %s, want each model once", got)
	}
}

func TestBadRequestDoesNotFallBack(t *testing.T) {
	server := &modelServer{down: map[string]int{"primary": 400}}
	client := fallbackClient(t, server)

	if _, err := client.Complete(context.Background(), userRequest()); err == nil {
		t.Fatal("expected the 400 to be returned")
	}
	if len(server.asked) != 1 {
		t.Errorf("models asked = %v; a bad request would fail on every model", server.asked)
	}
}

func TestEachTurnStartsWithThePrimaryModel(t *testing.T) {
	server := &modelServer{down: map[string]int{"primary": 503}}
	client := fallbackClient(t, server)
	_, _ = client.Complete(context.Background(), userRequest())

	server.mu.Lock()
	delete(server.down, "primary")
	server.asked = nil
	server.mu.Unlock()
	msg, _ := client.Complete(context.Background(), userRequest())

	if msg.Content != "from primary" {
		t.Errorf("msg = %+v; once the primary recovers it should be used again", msg)
	}
}

func TestTranscribeSendsTheAudioAndReturnsTheText(t *testing.T) {
	client, rec := newTestClient(t, reply{status: 200, body: `{"choices":[{"message":{"role":"assistant","content":"  salom dunyo \n"}}]}`})

	text, err := client.Transcribe(context.Background(), []byte("OggS-voice"))
	if err != nil || text != "salom dunyo" {
		t.Fatalf("text = %q, err = %v", text, err)
	}
	messages, _ := rec.requests[0]["messages"].([]any)
	content, _ := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %v, want the instruction and the audio", messages[0])
	}
	instruction := content[0].(map[string]any)["text"].(string)
	audio := content[1].(map[string]any)["input_audio"].(map[string]any)
	if !strings.Contains(instruction, "Transcribe") || audio["format"] != "mp3" || audio["data"] != "T2dnUy12b2ljZQ==" {
		t.Errorf("instruction = %q, audio = %v", instruction, audio)
	}
	if _, present := rec.requests[0]["tools"]; present {
		t.Error("a transcription request offered tools")
	}
}

// Gemini 3 signs each tool call and refuses the follow-up request unless the
// signature comes back with the call ("Function call is missing a
// thought_signature").
func TestToolCallSignatureIsKeptAndSentBack(t *testing.T) {
	client, rec := newTestClient(t,
		reply{status: 200, body: `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"generate_image","arguments":"{}"},
			 "extra_content":{"google":{"thought_signature":"SIG123"}}}]}}]}`},
		reply{status: 200, body: plainReply},
	)

	first, err := client.Complete(context.Background(), toolRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || !strings.Contains(string(first.ToolCalls[0].ExtraContent), "SIG123") {
		t.Fatalf("tool call = %+v, want the signature kept", first.ToolCalls)
	}

	stored, _ := json.Marshal(first.ToolCalls) // as storage keeps them
	var restored []ToolCall
	if err := json.Unmarshal(stored, &restored); err != nil {
		t.Fatal(err)
	}
	req := toolRequest()
	req.Messages = append(req.Messages, Message{Role: RoleAssistant, ToolCalls: restored},
		Message{Role: RoleTool, ToolCallID: "call_1", Content: "{}"})
	if _, err := client.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	sent, _ := json.Marshal(rec.requests[1]["messages"])
	if !strings.Contains(string(sent), `"thought_signature":"SIG123"`) {
		t.Errorf("follow-up request lost the signature: %s", sent)
	}
}
