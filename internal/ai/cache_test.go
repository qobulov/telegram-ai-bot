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

// geminiFake is Gemini's cache service and chat endpoint: it makes caches,
// and refuses a chat with a cache when refuse is set.
type geminiFake struct {
	mu      sync.Mutex
	made    []map[string]any
	chats   []map[string]any
	refuse  bool
	headers []http.Header
}

func (g *geminiFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	switch r.URL.Path {
	case "/v1beta/cachedContents":
		g.made = append(g.made, body)
		g.headers = append(g.headers, r.Header.Clone())
		_, _ = io.WriteString(w, `{"name":"cachedContents/c1"}`)
	case "/v1beta/openai/chat/completions":
		g.chats = append(g.chats, body)
		if _, cached := body["extra_body"]; cached && g.refuse {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"CachedContent not found"}}`)
			return
		}
		_, _ = io.WriteString(w, plainReply)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func cachedClient(t *testing.T) (*Client, *geminiFake, *time.Time) {
	t.Helper()
	fake := &geminiFake{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	c := NewClient(Options{BaseURL: server.URL + "/v1beta/openai", APIKey: testAPIKey, Model: "gemini-test", FallbackModels: []string{"gemini-other"}, MaxAttempts: 1})
	c.now = func() time.Time { return now }
	c.cache = &promptCache{base: server.URL + "/v1beta", key: testAPIKey, client: http.DefaultClient, now: c.now, entries: map[string]*cacheEntry{}}
	return c, fake, &now
}

func cachedRequest(system string) Request {
	return Request{
		Messages: []Message{{Role: RoleSystem, Content: system}, {Role: RoleUser, Content: "salom"}},
		Tools:    []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "get_weather", Description: "weather", Parameters: json.RawMessage(`{"type":"object"}`)}}},
	}
}

// A prompt is cached the second time it is asked for within cacheWarmup and
// used until its cache ends; the request then goes without its system
// prompt and tools, naming the cache.
func TestPromptCacheIsMadeOnTheSecondAsk(t *testing.T) {
	c, fake, now := cachedClient(t)
	system := strings.Repeat("persona ", 2000)
	for i := range 3 {
		if _, err := c.Complete(context.Background(), cachedRequest(system)); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
		*now = now.Add(time.Minute)
	}
	if len(fake.made) != 1 || fake.headers[0].Get("x-goog-api-key") != testAPIKey {
		t.Fatalf("caches made = %d", len(fake.made))
	}
	if made := fake.made[0]; made["model"] != "models/gemini-test" || made["ttl"] != "3600s" {
		t.Errorf("cache = %v", made)
	}
	if _, cached := fake.chats[0]["extra_body"]; cached || len(fake.chats[0]["messages"].([]any)) != 2 || fake.chats[0]["tools"] == nil {
		t.Errorf("first ask = %v, want it whole", fake.chats[0])
	}
	for _, chat := range fake.chats[1:] {
		extra, _ := chat["extra_body"].(map[string]any)
		google, _ := extra["google"].(map[string]any)
		messages := chat["messages"].([]any)
		if google["cached_content"] != "cachedContents/c1" || chat["tools"] != nil || len(messages) != 1 || messages[0].(map[string]any)["role"] != RoleUser {
			t.Errorf("cached ask = %v", chat)
		}
	}

	*now = now.Add(cacheTTL)
	if _, err := c.Complete(context.Background(), cachedRequest(system)); err != nil {
		t.Fatal(err)
	}
	if len(fake.made) != 1 {
		t.Errorf("a prompt asked for after an hour's silence made a cache")
	}
}

// A cache the provider refuses is dropped and the request sent whole; a
// short prompt, one that forbids tools, and a fallback model go whole.
func TestPromptCacheFallsBackWhole(t *testing.T) {
	c, fake, now := cachedClient(t)
	system := strings.Repeat("persona ", 2000)
	c.Complete(context.Background(), cachedRequest(system))
	fake.refuse = true
	if msg, err := c.Complete(context.Background(), cachedRequest(system)); err != nil || msg.Content != "Salom!" {
		t.Fatalf("a refused cache = %v, %v", msg, err)
	}
	if n := len(fake.chats); n != 3 || fake.chats[2]["extra_body"] != nil {
		t.Fatalf("after the refusal: %d chats, last %v", n, fake.chats[n-1])
	}
	*now = now.Add(time.Minute)
	c.Complete(context.Background(), cachedRequest(system))
	if len(fake.made) != 1 || fake.chats[3]["extra_body"] != nil {
		t.Errorf("a refused prompt was cached again within cacheBackoff")
	}

	c, fake, _ = cachedClient(t)
	short := cachedRequest("short persona")
	forbid := cachedRequest(system)
	forbid.ForbidTools = true
	for range 2 {
		c.Complete(context.Background(), short)
		c.Complete(context.Background(), forbid)
	}
	if len(fake.made) != 0 {
		t.Errorf("caches made for a short prompt or one forbidding tools: %d", len(fake.made))
	}
}

// Caching is for Gemini's OpenAI-compatible endpoint only.
func TestPromptCacheOnlyForGemini(t *testing.T) {
	if newPromptCache("https://openrouter.ai/api/v1", "k", http.DefaultClient, time.Now) != nil {
		t.Error("a cache for OpenRouter")
	}
	p := newPromptCache("https://generativelanguage.googleapis.com/v1beta/openai", "k", http.DefaultClient, time.Now)
	if p == nil || p.base != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("Gemini's cache = %+v", p)
	}
}
