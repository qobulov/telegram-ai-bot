package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// TestRoutingEval runs real group turns through the router and the chat
// model against the live Gemini API, to see what routing costs and what it
// answers. It runs only with ROUTING_EVAL_CASES (a JSON file of cases),
// ROUTING_EVAL_KEY and ROUTING_EVAL_OUT set. Tools are offered but never
// run, so nothing reaches Telegram or the database. A turn the router
// sends to the full path is not asked of the main model unless
// ROUTING_EVAL_FULL=1: that path is today's, whose answer the case holds.
// ROUTING_EVAL_ROUTER=off measures today's path itself.
func TestRoutingEval(t *testing.T) {
	casesPath, key, out := os.Getenv("ROUTING_EVAL_CASES"), os.Getenv("ROUTING_EVAL_KEY"), os.Getenv("ROUTING_EVAL_OUT")
	if casesPath == "" || key == "" || out == "" {
		t.Skip("ROUTING_EVAL_CASES, ROUTING_EVAL_KEY and ROUTING_EVAL_OUT are not all set")
	}
	raw, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	var cases []evalCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	usage := &usageLog{}
	logger := slog.New(usage)
	cfg := evalConfig(t, key)
	router, chat := chatRouting(cfg, logger)
	var main agent.Model = fullPathSkipped{}
	if os.Getenv("ROUTING_EVAL_FULL") == "1" {
		main = ai.NewClient(ai.Options{BaseURL: cfg.AIBaseURL, APIKey: cfg.AIAPIKey, Model: cfg.Model, MaxTokens: cfg.MaxOutputTokens,
			ReasoningEffort: cfg.ReasoningEffort, MaxAttempts: 2, Logger: logger})
	}
	bot := Bot{Client: telegram.NewClient(telegram.Options{Token: "1:eval"}), User: telegram.User{ID: 1, Username: "evalbot"}}
	available := append(builtinTools(cfg, nil, bot, tools.NewModeration(tools.ModerationOptions{}).Tool()), liveDataTools(cfg, bot)...)
	registry, err := tools.NewRegistry(cfg.IsAdmin, logger, available...)
	if err != nil {
		t.Fatal(err)
	}

	// ROUTING_EVAL_PAUSE spaces the cases, for a key whose quota counts
	// requests a minute.
	pause, _ := time.ParseDuration(os.Getenv("ROUTING_EVAL_PAUSE"))
	var results []evalResult
	for i, c := range cases {
		if i > 0 {
			time.Sleep(pause)
		}
		store := &evalStore{history: c.history()}
		a := agent.New(agent.Options{Store: store, Model: main, Tools: evalTools{registry}, Router: router, ChatModel: chat, Name: cfg.BotDisplayName,
			Persona: cfg.PersonaPrompt, GroupNote: cfg.GroupNote, MaxHistory: 20, MaxToolCalls: cfg.MaxToolCallsPerTurn})
		caller := tools.Caller{ChatID: -100, UserID: 5000, Human: true, UpdateID: int64(c.ID), Words: c.Text}
		if c.fromOwner() {
			caller.UserID, caller.Owner = 1688909825, true
		}
		ctx := tools.WithTurn(context.Background(), caller, &tools.Sink{})
		usage.reset()
		started := time.Now()
		answer, err := a.Reply(ctx, agent.Request{Scope: storage.Scope{ChatID: -100}, UserID: caller.UserID, UpdateID: int64(c.ID), Text: c.Text, Context: c.Context, Group: true})
		r := evalResult{ID: c.ID, Kind: c.Kind, UsedTool: c.UsedTool, Text: c.Text, ProdAnswer: c.ProdAnswer, Route: answer.Route,
			Answer: answer.Text, Reaction: answer.Reaction, Sticker: answer.Sticker, GIF: answer.GIF, Seconds: time.Since(started).Seconds(), Calls: usage.calls()}
		if err != nil {
			r.Error = err.Error()
		}
		if errors.Is(err, errFullPathSkipped) {
			r.Route, r.Error = "full (not asked)", ""
		}
		results = append(results, r)
		t.Logf("%d %s route=%s %.1fs %q", c.ID, c.Kind, r.Route, r.Seconds, r.Answer)
	}
	encoded, _ := json.MarshalIndent(results, "", " ")
	if err := os.WriteFile(out, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

type evalCase struct {
	ID         int    `json:"id"`
	Kind       string `json:"kind"`
	UsedTool   bool   `json:"used_tool"`
	Text       string `json:"text"`
	ProdAnswer string `json:"prod_answer"`
	History    []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"history"`
	Context string `json:"context"`
}

func (c evalCase) history() []ai.Message {
	messages := make([]ai.Message, len(c.History))
	for i, h := range c.History {
		messages[i] = ai.Message{Role: h.Role, Content: h.Content}
	}
	return messages
}

// fromOwner reports a message an owner wrote, by the mark the service puts
// after their name.
func (c evalCase) fromOwner() bool {
	lines := strings.Split(strings.TrimSpace(c.Text), "\n")
	speaker, _, _ := strings.Cut(lines[len(lines)-1], ": ")
	return strings.Contains(speaker, "[owner]")
}

type evalResult struct {
	ID         int         `json:"id"`
	Kind       string      `json:"kind"`
	UsedTool   bool        `json:"used_tool"`
	Text       string      `json:"text"`
	ProdAnswer string      `json:"prod_answer"`
	Route      string      `json:"route"`
	Answer     string      `json:"answer"`
	Reaction   string      `json:"reaction,omitempty"`
	Sticker    string      `json:"sticker,omitempty"`
	GIF        string      `json:"gif,omitempty"`
	Error      string      `json:"error,omitempty"`
	Seconds    float64     `json:"seconds"`
	Calls      []evalUsage `json:"calls"`
}

type evalUsage struct {
	Model     string `json:"model"`
	Prompt    int64  `json:"prompt"`
	Cached    int64  `json:"cached"`
	Output    int64  `json:"output"`
	Reasoning int64  `json:"reasoning"`
}

// evalConfig is production's configuration as far as the prompt and the
// tools go: its owners and the keys that add tools, with the eval's key.
func evalConfig(t *testing.T, key string) config.Config {
	env := map[string]string{
		"TELEGRAM_BOT_TOKEN": "1:eval", "GEMINI_API_KEY": key, "DB_POSTGRES_URL": "postgres://eval@localhost/eval",
		"BOT_OWNER_USERNAME": "pronium,TursunoffM", "BOT_OWNER_IDS": "1688909825,1047217990",
		"FOOTBALL_DATA_TOKEN": "x", "TAVILY_API_KEY": "x", "TMDB_TOKEN": "x", "YOUTUBE_API_KEY": "x",
		"SIGHTENGINE_API_USER": "x", "SIGHTENGINE_API_SECRET": "x", "GOOGLE_SAFE_BROWSING_KEY": "x",
		"ROUTER_MODEL": cmp.Or(os.Getenv("ROUTING_EVAL_ROUTER"), "gemini-3.5-flash-lite"), "CHAT_MODEL": os.Getenv("ROUTING_EVAL_CHAT_MODEL"),
	}
	if env["ROUTER_MODEL"] == "off" {
		// Today's path, every message to the main model with the tools.
		env["ROUTER_MODEL"] = ""
	}
	cfg, err := config.Load(func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// evalTools offers the real tools but runs none of them.
type evalTools struct{ registry *tools.Registry }

func (e evalTools) Definitions(ctx context.Context, callerID int64) []ai.ToolDefinition {
	return e.registry.Definitions(ctx, callerID)
}

func (evalTools) Execute(context.Context, int64, string, string) string {
	return `{"ok":false,"error":{"code":"unavailable","message":"The tool did not run in this test."}}`
}

var errFullPathSkipped = errors.New("eval: the full path was not asked")

// fullPathSkipped stands for the main model when only routing is measured.
type fullPathSkipped struct{}

func (fullPathSkipped) Complete(context.Context, ai.Request) (ai.Message, error) {
	return ai.Message{}, errFullPathSkipped
}

// evalStore holds one case's history and keeps nothing.
type evalStore struct{ history []ai.Message }

func (s *evalStore) History(context.Context, storage.Scope, int) ([]ai.Message, error) {
	return s.history, nil
}

func (s *evalStore) SaveTurn(context.Context, storage.Turn) (int64, error) { return 1, nil }

func (s *evalStore) UpdateProcessed(context.Context, int64) (bool, error) { return false, nil }

// usageLog collects the "model usage" lines the clients log.
type usageLog struct {
	mu    sync.Mutex
	usage []evalUsage
}

func (u *usageLog) Enabled(context.Context, slog.Level) bool { return true }
func (u *usageLog) WithAttrs([]slog.Attr) slog.Handler       { return u }
func (u *usageLog) WithGroup(string) slog.Handler            { return u }

func (u *usageLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "model usage" {
		return nil
	}
	var e evalUsage
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "model":
			e.Model = a.Value.String()
		case "prompt_tokens":
			e.Prompt = a.Value.Int64()
		case "cached_tokens":
			e.Cached = a.Value.Int64()
		case "completion_tokens":
			e.Output = a.Value.Int64()
		case "reasoning_tokens":
			e.Reasoning = a.Value.Int64()
		}
		return true
	})
	u.mu.Lock()
	u.usage = append(u.usage, e)
	u.mu.Unlock()
	return nil
}

func (u *usageLog) reset() {
	u.mu.Lock()
	u.usage = nil
	u.mu.Unlock()
}

func (u *usageLog) calls() []evalUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]evalUsage(nil), u.usage...)
}
