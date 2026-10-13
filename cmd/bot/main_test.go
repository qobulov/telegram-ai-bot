package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
)

const e2eUser = int64(777001)

// fakeTelegram delivers one text message and records what the bot sends back.
type fakeTelegram struct {
	mu        sync.Mutex
	delivered bool
	sent      chan string
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		var params struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&params)
		f.sent <- params.Text
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
		return
	}
	f.mu.Lock()
	first := !f.delivered
	f.delivered = true
	f.mu.Unlock()
	if !first {
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":900001,"message":{"message_id":1,
		"from":{"id":777001,"is_bot":false},"chat":{"id":777001,"type":"private"},"text":"what is 2+2?"}}]}`)
}

// fakeOpenRouter asks for the calculator, then answers with its result.
func fakeOpenRouter(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		last := req.Messages[len(req.Messages)-1]
		if last.Role != "tool" {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"calculator","arguments":"{\"expression\":\"2+2\"}"}}]}}]}`)
			return
		}
		if !strings.Contains(last.Content, `"result":4`) {
			t.Errorf("tool result sent to the model = %s", last.Content)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"2+2 = 4"}}]}`)
	})
}

// TestServeAnswersMessageEndToEnd wires the real packages together against
// fake Telegram and OpenRouter servers and a real PostgreSQL database.
func TestServeAnswersMessageEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping end-to-end test")
	}
	telegramAPI := &fakeTelegram{sent: make(chan string, 4)}
	telegramServer := httptest.NewServer(telegramAPI)
	defer telegramServer.Close()
	modelServer := httptest.NewServer(fakeOpenRouter(t))
	defer modelServer.Close()

	cfg, err := config.Load(func(key string) string {
		return map[string]string{
			"TELEGRAM_BOT_TOKEN":  "1:test",
			"TELEGRAM_API_URL":    telegramServer.URL,
			"OPENROUTER_API_KEY":  "test",
			"OPENROUTER_BASE_URL": modelServer.URL,
			"DATABASE_URL":        databaseURL,
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := storage.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Reset(ctx, e2eUser); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	served := make(chan error, 1)
	go func() { served <- serve(ctx, cfg, store, logger) }()

	select {
	case answer := <-telegramAPI.sent:
		if answer != "2+2 = 4" {
			t.Errorf("bot sent %q", answer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the bot did not answer")
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("serve returned %v after shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not shut down")
	}

	history, err := store.History(context.Background(), e2eUser, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[2].Role != "tool" || history[3].Content != "2+2 = 4" {
		t.Errorf("saved history = %+v", history)
	}
}

// Migrations are run by an operator with the owner role. They must not need
// the bot's other secrets.
func TestMigrateNeedsOnlyDatabaseURL(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping migrate test")
	}
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	if err := run([]string{"migrate"}); err != nil {
		t.Fatalf("run migrate: %v", err)
	}
}

func TestMigrateWithoutDatabaseURLNamesTheVariable(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	err := run([]string{"migrate"})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v, want mention of DATABASE_URL", err)
	}
}
