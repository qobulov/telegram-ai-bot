package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Gemini's explicit cache of a request's system prompt and tools, as the
// owners asked on 2026-11-24 to pay less for input: every turn sends the
// same ~13.5k tokens of persona and tool definitions, and Gemini's implicit
// cache, which needs requests close together, hit none of them in
// production. A cached token costs a tenth of an uncached one, and a cache
// is paid for by the hour, so one is made only for a prompt asked for twice
// within cacheWarmup: a quiet hour makes none.
const (
	// cacheTTL is how long a cache lives; it is used until cacheMargin
	// before it ends, then made anew if still asked for.
	cacheTTL    = time.Hour
	cacheMargin = 5 * time.Minute
	// cacheWarmup is how soon a prompt must be asked for again to be cached.
	cacheWarmup = 10 * time.Minute
	// cacheBackoff is how long a prompt whose cache could not be made or
	// used goes uncached.
	cacheBackoff = 10 * time.Minute
	// cacheCreateTimeout bounds making a cache, which the turn waits for.
	cacheCreateTimeout = 8 * time.Second
	// cacheMinChars is the shortest prompt worth a cache: Gemini caches no
	// fewer than 1024 tokens, and a short one saves too little.
	cacheMinChars = 8000
)

// promptCache keeps the caches made, by the hash of model, system prompt
// and tools. It is safe for concurrent use.
type promptCache struct {
	base   string // the native API, as https://.../v1beta
	key    string
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	name     string    // "cachedContents/...", "" until made
	expires  time.Time // when name stops being used
	lastSeen time.Time
	failed   time.Time // when a failure stops holding it back
}

// newPromptCache is a cache for a Gemini OpenAI-compatible base URL, nil
// for any other provider's.
func newPromptCache(baseURL, key string, client *http.Client, now func() time.Time) *promptCache {
	base, ok := strings.CutSuffix(baseURL, "/openai")
	if !ok || !strings.Contains(base, "generativelanguage.googleapis.com") {
		return nil
	}
	return &promptCache{base: base, key: key, client: client, now: now, entries: map[string]*cacheEntry{}}
}

// cacheKey identifies what a cache holds.
func cacheKey(model, system string, tools []ToolDefinition) string {
	h := sha256.New()
	tb, _ := json.Marshal(tools)
	fmt.Fprintf(h, "%s\x00%s\x00", model, system)
	h.Write(tb)
	return hex.EncodeToString(h.Sum(nil))
}

// name is the cache to use for system and tools with model, making it when
// the prompt was asked for within cacheWarmup; "" to send it uncached.
func (p *promptCache) name(ctx context.Context, model, system string, tools []ToolDefinition) (string, string) {
	if len(system) < cacheMinChars || len(tools) == 0 {
		return "", ""
	}
	key := cacheKey(model, system, tools)
	now := p.now()
	p.mu.Lock()
	e := p.entries[key]
	if e == nil {
		e = &cacheEntry{}
		p.entries[key] = e
	}
	seen := e.lastSeen
	e.lastSeen = now
	switch {
	case e.name != "" && now.Before(e.expires):
		name := e.name
		p.mu.Unlock()
		return key, name
	case now.Before(e.failed), seen.IsZero(), now.Sub(seen) > cacheWarmup:
		p.mu.Unlock()
		return key, ""
	}
	p.mu.Unlock()

	name, err := p.create(ctx, model, system, tools)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		e.failed = now.Add(cacheBackoff)
		return key, ""
	}
	e.name, e.expires = name, now.Add(cacheTTL-cacheMargin)
	return key, name
}

// drop forgets key's cache, which the provider refused, and holds the
// prompt back from caching for cacheBackoff.
func (p *promptCache) drop(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[key]; e != nil {
		e.name, e.failed = "", p.now().Add(cacheBackoff)
	}
}

// create makes a cache of system and tools for model.
func (p *promptCache) create(ctx context.Context, model, system string, tools []ToolDefinition) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cacheCreateTimeout)
	defer cancel()
	decls := make([]map[string]any, len(tools))
	for i, t := range tools {
		params := t.Function.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		decls[i] = map[string]any{"name": t.Function.Name, "description": t.Function.Description, "parametersJsonSchema": params}
	}
	body, err := json.Marshal(map[string]any{
		"model":             "models/" + model,
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": system}}},
		"tools":             []any{map[string]any{"functionDeclarations": decls}},
		"ttl":               fmt.Sprintf("%ds", int(cacheTTL.Seconds())),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/cachedContents", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.key)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp, raw)
	}
	var made struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &made); err != nil || made.Name == "" {
		return "", errors.New("ai: the cache made has no name")
	}
	return made.Name, nil
}

// cachedPayload is payload sent with cache name: without the system prompt
// and the tools, which the cache holds.
type cachedPayload struct {
	chatRequest
	ExtraBody map[string]any `json:"extra_body"`
}

func withCache(payload chatRequest, name string) cachedPayload {
	payload.Messages = payload.Messages[1:]
	payload.Tools = nil
	return cachedPayload{chatRequest: payload, ExtraBody: map[string]any{"google": map[string]any{"cached_content": name}}}
}

// isCacheRefused reports a request the provider refused for its cache: a
// 4xx other than a rate limit, as for a cache gone or not this model's.
func isCacheRefused(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusTooManyRequests
}
