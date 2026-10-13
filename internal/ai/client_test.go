package ai

import (
	"context"
	"encoding/json"
	"errors"
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
