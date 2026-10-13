package telegram

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

const testToken = "123456:test-secret-token"

type call struct {
	method string
	params map[string]any
}

// fakeAPI is a scripted Telegram Bot API. Each request consumes the next
// reply; the last reply repeats.
type fakeAPI struct {
	mu      sync.Mutex
	replies []string
	calls   []call
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var params map[string]any
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &params)
	method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
	f.calls = append(f.calls, call{method: method, params: params})

	next := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, next)
}

func (f *fakeAPI) recorded() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func newTestClient(t *testing.T, replies ...string) (*Client, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{replies: replies}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := NewClient(Options{
		Token:     testToken,
		BaseURL:   server.URL,
		BaseDelay: time.Millisecond,
	})
	return client, api
}

const okEmpty = `{"ok":true,"result":[]}`
const okSent = `{"ok":true,"result":{"message_id":1}}`

func TestGetUpdatesParsesTextMessage(t *testing.T) {
	client, api := newTestClient(t, `{"ok":true,"result":[{"update_id":7,"message":{
		"message_id":3,"from":{"id":42,"is_bot":false},"chat":{"id":42,"type":"private"},"text":"salom"}}]}`)

	updates, err := client.GetUpdates(context.Background(), 5, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %+v", updates)
	}
	got := updates[0]
	if got.UpdateID != 7 || got.Message == nil || got.Message.Text != "salom" {
		t.Fatalf("update = %+v", got)
	}
	if got.Message.From == nil || got.Message.From.ID != 42 || got.Message.Chat.Type != "private" {
		t.Errorf("message = %+v", got.Message)
	}

	sent := api.recorded()[0]
	if sent.method != "getUpdates" || sent.params["offset"] != float64(5) {
		t.Errorf("call = %+v", sent)
	}
	if allowed, _ := sent.params["allowed_updates"].([]any); len(allowed) != 1 || allowed[0] != "message" {
		t.Errorf("allowed_updates = %v", sent.params["allowed_updates"])
	}
}

func TestAPIErrorIsDecoded(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`)

	_, err := client.GetUpdates(context.Background(), 0, 0)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != 429 || apiErr.RetryAfter != 3*time.Second || apiErr.Description != "Too Many Requests" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestSendMessageSendsPlainText(t *testing.T) {
	client, api := newTestClient(t, okSent)

	if err := client.SendMessage(context.Background(), 42, "hello *world*"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	sent := api.recorded()[0]
	if sent.method != "sendMessage" || sent.params["chat_id"] != float64(42) || sent.params["text"] != "hello *world*" {
		t.Errorf("call = %+v", sent)
	}
	if _, present := sent.params["parse_mode"]; present {
		t.Error("parse_mode must not be sent")
	}
}

func TestSendMessageSplitsLongText(t *testing.T) {
	client, api := newTestClient(t, okSent)
	text := strings.Repeat("a", 4000) + "\n" + strings.Repeat("b", 999)

	if err := client.SendMessage(context.Background(), 42, text); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	calls := api.recorded()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	first, _ := calls[0].params["text"].(string)
	second, _ := calls[1].params["text"].(string)
	if !strings.HasPrefix(first, "aaa") || !strings.HasPrefix(second, "bbb") {
		t.Errorf("chunks out of order: %q..., %q...", first[:3], second[:3])
	}
}

func TestSendMessageSkipsBlankText(t *testing.T) {
	client, api := newTestClient(t, okSent)

	if err := client.SendMessage(context.Background(), 42, " \n "); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls := api.recorded(); len(calls) != 0 {
		t.Errorf("calls = %+v, want none", calls)
	}
}

func TestSendMessageRetriesRateLimit(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":false,"error_code":429,"description":"Too Many Requests"}`,
		okSent,
	)

	if err := client.SendMessage(context.Background(), 42, "hi"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls := api.recorded(); len(calls) != 2 {
		t.Errorf("calls = %d, want 2", len(calls))
	}
}

func TestSendMessageGivesUpOnPersistentRateLimit(t *testing.T) {
	client, api := newTestClient(t, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`)

	err := client.SendMessage(context.Background(), 42, "hi")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 429 {
		t.Fatalf("err = %v", err)
	}
	if calls := api.recorded(); len(calls) != 3 {
		t.Errorf("calls = %d, want 3", len(calls))
	}
}

func TestSendMessageDoesNotRetryOtherErrors(t *testing.T) {
	client, api := newTestClient(t, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)

	if err := client.SendMessage(context.Background(), 42, "hi"); err == nil {
		t.Fatal("expected error")
	}
	if calls := api.recorded(); len(calls) != 1 {
		t.Errorf("calls = %d, want 1", len(calls))
	}
}

func update(id int) string {
	return `{"update_id":` + string(rune('0'+id)) + `,"message":{"message_id":1,"from":{"id":1},"chat":{"id":1,"type":"private"},"text":"m"}}`
}

func TestPollDeliversUpdatesAndAdvancesOffset(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":true,"result":[`+update(1)+`,`+update(2)+`]}`,
		`{"ok":false,"error_code":502,"description":"Bad Gateway"}`,
		`{"ok":true,"result":[`+update(3)+`]}`,
		okEmpty,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []int64
	err := client.Poll(ctx, func(u Update) {
		got = append(got, u.UpdateID)
		if len(got) == 3 {
			cancel()
		}
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("updates = %v", got)
	}

	calls := api.recorded()
	wantOffsets := []float64{0, 3, 3}
	for i, want := range wantOffsets {
		if calls[i].params["offset"] != want {
			t.Errorf("call %d offset = %v, want %v", i, calls[i].params["offset"], want)
		}
	}
}

func TestPollStopsOnInvalidToken(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)

	err := client.Poll(context.Background(), func(Update) {})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 401 {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	client := NewClient(Options{Token: testToken, BaseURL: "http://127.0.0.1:1"})

	_, err := client.GetUpdates(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "test-secret") {
		t.Errorf("error leaks token: %v", err)
	}
}
