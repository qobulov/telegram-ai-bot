# Telegram AI Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Telegram assistant bot in Go that answers private messages through OpenRouter with tool calling and per-user memory in PostgreSQL.

**Architecture:** A modular monolith. `service` dispatches Telegram updates to a fixed worker pool sharded by user ID; `agent` runs one turn (history → model → tools → save); `ai`, `telegram`, `tools` and `storage` are leaf packages behind small interfaces declared by their consumers.

**Tech Stack:** Go 1.27, standard library, `github.com/jackc/pgx/v5`, PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-10-13-telegram-ai-agent-design.md`

**Note on format:** this plan fixes every file, exported signature, behaviour and test case, but does not duplicate full function bodies. It is written to be executed by its author in the same session; the spec and the signatures below are the contract.

## Global Constraints

- Module path: `telegram-ai-bot`. Only third-party dependency: `github.com/jackc/pgx/v5`.
- No package-level mutable state. `context.Context` is the first parameter of every call that does I/O.
- All SQL parameterized. `pgx` is used directly; this is an owner-approved exception to the workspace `CLAUDE.md` Ucode rule.
- Never log or return to users: bot token, API key, database URL, message text.
- Tests need no network and no credentials. Storage tests skip unless `TEST_db_POSTGRES_URL` is set.
- Every task ends with `gofmt -l .` empty, `go vet ./...` clean, `go test -race ./...` green, and a commit on `feat/telegram-ai-agent`.
- Functions stay short, errors are sentinel values or typed, early returns over nesting (workspace clean-Go rules).

## Review Focus

1. The model sends a tool call with `arguments: ""` (common for no-argument tools) → treated as `{}`, not as malformed JSON. Test in Task 4.
2. An update with no `from`, an edited message, a channel post or a group message → ignored without a panic or a reply. Test in Task 7.
3. The model returns `content: null` with no tool calls → the user gets the generic error and nothing is saved. Test in Task 6.
4. A reply containing emoji or other non-BMP text right at the split boundary → no chunk exceeds 4096 UTF-16 units and no rune is cut. Test in Task 3.
5. Calculator input such as `1e999`, 200 nested parentheses, `2 +`, or `1/(2-2)` → a structured error, never a panic or `Inf`. Test in Task 4.

---

## File map

```text
go.mod, go.sum
.gitignore, .env.example
Makefile, Dockerfile, docker-compose.yml, telegram-ai-agent.service
README.md
cmd/bot/main.go
internal/config/config.go, dotenv.go, config_test.go
internal/ai/types.go, client.go, client_test.go
internal/telegram/types.go, client.go, poll.go, split.go, *_test.go
internal/tools/registry.go, time.go, calculator.go, help.go, *_test.go
internal/storage/store.go, migrate.go, conversations.go, store_test.go
internal/storage/migrations/0001_init.sql
internal/agent/agent.go, history.go, agent_test.go
internal/service/service.go, commands.go, limiter.go, texts.go, *_test.go
```

---

### Task 1: Scaffold and configuration

**Files:** `go.mod`, `.gitignore`, `.env.example`, `internal/config/{config.go,dotenv.go,config_test.go}`

**Produces:**

```go
type Config struct {
    TelegramToken, TelegramAPIURL       string
    OpenRouterAPIKey, OpenRouterBaseURL string
    Model, DatabaseURL, SystemPrompt    string
    MaxHistoryMessages, MaxToolCallsPerTurn, MaxConcurrentRequests int
    UserRateLimitPerMinute, MaxOutputTokens, MaxInputChars, RetentionDays int
    RequestTimeout time.Duration
    LogLevel       slog.Level
    AdminUserIDs   map[int64]bool
    AdminCommands  map[string]bool // keys like "/model"
}
func Load(getenv func(string) string) (Config, error)
func LoadDotEnv(path string) error // missing file is not an error; never overrides set variables
func (c Config) IsAdmin(userID int64) bool
```

- [ ] Tests first (`config_test.go`, using a `map[string]string` getenv): defaults applied when only the three required variables are set; each missing required variable yields an error naming it; `MAX_HISTORY_MESSAGES=abc`, `=0`, `=-1` rejected; `RETENTION_DAYS=0` accepted; `ADMIN_USER_IDS="1, 2"` parsed, `"1,x"` rejected; `ADMIN_COMMANDS="model,/reset"` normalised to `/model`, `/reset`; an invalid `LOG_LEVEL` rejected; error text never contains the token, key or database URL values; `LoadDotEnv` handles comments, blank lines, `KEY="quoted value"` and does not override an existing variable.
- [ ] Run `go test ./internal/config/` — fails to compile.
- [ ] Implement. `.env.example` documents every variable from the spec table. `.gitignore` covers `.env`, `/bin/`, `*.dump`.
- [ ] Verify and commit.

### Task 2: OpenRouter client

**Files:** `internal/ai/{types.go,client.go,client_test.go}`

**Produces:**

```go
type Message struct {
    Role       string     `json:"role"`
    Content    string     `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
}
type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`
    Function FunctionCall `json:"function"`
}
type FunctionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
}
type ToolDefinition struct {
    Type     string             `json:"type"` // "function"
    Function FunctionDefinition `json:"function"`
}
type FunctionDefinition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
}
type Request struct {
    Messages     []Message
    Tools        []ToolDefinition
    ForbidTools  bool // send tool_choice "none"
}
type APIError struct { Status int; Message string; RetryAfter time.Duration }
var ErrEmptyResponse = errors.New("ai: empty response")

type Options struct {
    BaseURL, APIKey, Model string
    MaxTokens, MaxAttempts int        // MaxAttempts default 3
    HTTPClient *http.Client
    BaseDelay  time.Duration          // default 500ms; tests use 1ms
}
func NewClient(Options) *Client
func (c *Client) Complete(ctx context.Context, req Request) (Message, error)
```

Behaviour: retry 429, 5xx and transport errors with exponential backoff plus jitter, capped by `Retry-After` when present and by the context; no retry on other 4xx; an `error` object in a 200 body becomes an `APIError`; response body read through `io.LimitReader` (1 MiB); on a tools-unsupported rejection (400/404 whose message contains "tool") retry once without tools and remember it in an `atomic.Bool`.

- [ ] Tests first with `httptest.Server`: request carries `Authorization: Bearer k`, model, `max_tokens`, tools; plain reply decoded; tool-call reply decoded with `content: null`; 429 with `Retry-After: 0` then 200 succeeds; 500, 500, 200 succeeds on attempt 3; 500 forever stops at `MaxAttempts` and returns `APIError{Status: 500}`; 400 not retried (one request seen); in-body error returned; empty `choices` → `ErrEmptyResponse`; cancelled context returns promptly with `context.Canceled`; tools-unsupported 404 → second request has no `tools` key and later calls omit tools; `ForbidTools` sends `tool_choice: "none"`; the API key does not appear in any returned error string.
- [ ] Run — fails. Implement. Verify and commit.

### Task 3: Telegram client

**Files:** `internal/telegram/{types.go,client.go,poll.go,split.go,client_test.go,split_test.go}`

**Produces:**

```go
type Update struct { UpdateID int64 `json:"update_id"`; Message *Message `json:"message"` }
type Message struct { MessageID int64; From *User; Chat Chat; Text string }
type User struct { ID int64 `json:"id"`; IsBot bool `json:"is_bot"` }
type Chat struct { ID int64 `json:"id"`; Type string `json:"type"` }
type APIError struct { Code int; Description string; RetryAfter time.Duration }

func NewClient(token, baseURL string, hc *http.Client) *Client
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error)
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error
func (c *Client) Poll(ctx context.Context, handle func(Update)) error
func Split(text string, limit int) []string // limit in UTF-16 units
const MessageLimit = 4096
```

Behaviour: `SendMessage` splits with `Split`, sends chunks in order, and on 429 waits `retry_after` and retries that chunk up to 3 times. `Poll` loops `GetUpdates`, calls `handle` for each update, advances the offset to `update_id+1`, backs off 1s→30s with jitter on errors (reset on success), waits `retry_after` on 429, and returns the error on 401/404 (bad token) or `nil` on context cancellation. Errors never include the token: URLs are redacted before wrapping (`*url.Error` carries the URL).

- [ ] Tests first: `GetUpdates` sends `offset`, `timeout`, `allowed_updates` and parses a text update; `ok:false` becomes `APIError` with `retry_after`; `SendMessage` posts `chat_id` and `text` with no `parse_mode`; a 5000-character text arrives as two requests in order; 429 then OK is retried; `Poll` delivers updates from two batches with increasing offset and stops on cancel; `Poll` returns on 401; a transport error's text does not contain the token. `Split`: short text unchanged; empty text → no chunks; splits at the last newline before the limit; a single 9000-unit line splits hard; a string of 3000 emoji (2 units each) yields chunks ≤ 4096 units that rejoin to the original (Review Focus 4).
- [ ] Run — fails. Implement. Verify and commit.

### Task 4: Tools

**Files:** `internal/tools/{registry.go,time.go,calculator.go,help.go,registry_test.go,calculator_test.go,time_test.go}`

**Consumes:** `ai.ToolDefinition`.

**Produces:**

```go
type Tool struct {
    Name, Description string
    Schema    json.RawMessage
    AdminOnly bool
    Timeout   time.Duration
    Run       func(ctx context.Context, args json.RawMessage) (any, error)
}
func NewRegistry(isAdmin func(int64) bool, tools ...Tool) (*Registry, error) // rejects duplicate or empty names
func (r *Registry) Definitions(callerID int64) []ai.ToolDefinition          // hides admin-only tools from non-admins
func (r *Registry) Execute(ctx context.Context, callerID int64, name, args string) string
func Builtin(helpText string) []Tool
func Evaluate(expression string) (float64, error)
```

Result envelope: `{"ok":true,"result":…}` or `{"ok":false,"error":{"code":"…","message":"…"}}` with codes `unknown_tool`, `forbidden`, `invalid_arguments`, `timeout`, `output_too_large`, `execution_failed`. Empty `args` is treated as `{}`. `Run` errors of type `*ArgumentError` map to `invalid_arguments`; other errors map to `execution_failed` with a generic message (details logged by the caller is unnecessary — tools here have no secrets, so the error text is returned). Tools decode with `json.Decoder.DisallowUnknownFields`.

Calculator grammar: `expr = term {("+"|"-") term}`, `term = factor {("*"|"/") factor}`, `factor = ["-"|"+"] factor | number | "(" expr ")"`. Limits: 200 characters, depth 32.

- [ ] Tests first. Registry: unknown tool; admin-only tool refused for a non-admin and absent from their `Definitions`, allowed for an admin; a tool that blocks past its timeout → `timeout`; 5000-byte output → `output_too_large`; args `{bad` → `invalid_arguments`; args `""` runs with `{}` (Review Focus 1); duplicate names rejected by `NewRegistry`. Calculator table: `1+2*3=7`, `(1+2)*3=9`, `-4+10=6`, `2*-3=-6`, `10/4=2.5`, `1.5+1.5=3`, ` 2 * ( 3 + 4 ) =14`; errors for `""`, `2 +`, `(1`, `1)`, `2 ** 3`, `abc`, `1/0`, `1/(2-2)`, `1e999`, 200 nested parentheses, a 500-character expression (Review Focus 5). Time: no argument returns UTC in RFC 3339; `Asia/Tashkent` accepted; `Mars/Olympus` and `{"timezone":"UTC","extra":1}` rejected. Help: returns the supplied text.
- [ ] Run — fails. Implement. Verify and commit.

### Task 5: Storage and migrations

**Files:** `internal/storage/{store.go,migrate.go,conversations.go,store_test.go}`, `internal/storage/migrations/0001_init.sql`

**Consumes:** `ai.Message` (storage imports `ai` for this type only; update the spec's dependency sentence accordingly).

**Produces:**

```go
func Open(ctx context.Context, databaseURL string) (*Store, error) // pool + ping
func (s *Store) Close()
func (s *Store) Ping(ctx context.Context) error
func (s *Store) Migrate(ctx context.Context) (applied int, err error)
func (s *Store) CheckSchema(ctx context.Context) error // ErrSchemaOutdated
func (s *Store) History(ctx context.Context, userID int64, limit int) ([]ai.Message, error) // oldest first
func (s *Store) SaveTurn(ctx context.Context, userID, updateID int64, msgs []ai.Message) error
func (s *Store) UpdateProcessed(ctx context.Context, updateID int64) (bool, error)
func (s *Store) Reset(ctx context.Context, userID int64) error
func (s *Store) DeleteIdle(ctx context.Context, olderThan time.Duration) (int64, error)
var ErrDuplicateUpdate, ErrSchemaOutdated
```

`SaveTurn` runs in one transaction: upsert user (`last_seen_at`), upsert conversation (`updated_at`), insert messages in order with `update_id` on the first. A unique violation on `update_id` returns `ErrDuplicateUpdate`. `Migrate` holds `pg_advisory_lock`, applies each embedded file in its own transaction and records the version. Schema is exactly the spec's.

- [ ] Tests first (skip without `TEST_db_POSTGRES_URL`; each test migrates and truncates): migrate twice applies 1 then 0; `CheckSchema` passes after migrate; `SaveTurn` then `History` round-trips roles, content, tool calls and tool-call IDs in order; `History` limit returns the newest N, oldest first; user A never sees user B's messages; `Reset` removes only the caller's history and is a no-op for an unknown user; the same `updateID` twice → `ErrDuplicateUpdate` and no extra rows; `UpdateProcessed` true/false; `DeleteIdle` removes a conversation with an old `updated_at` and keeps a fresh one; the role check constraint rejects `role='system'`.
- [ ] Start a throwaway database: `docker run -d --rm --name tgbot-test-pg -e POSTGRES_PASSWORD=test -p 127.0.0.1:55432:5432 postgres:18` and run with `TEST_db_POSTGRES_URL=postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable`.
- [ ] Run — fails. Implement. Verify and commit.

### Task 6: Agent

**Files:** `internal/agent/{agent.go,history.go,agent_test.go}`

**Produces:**

```go
type Store interface {
    History(ctx context.Context, userID int64, limit int) ([]ai.Message, error)
    SaveTurn(ctx context.Context, userID, updateID int64, msgs []ai.Message) error
    UpdateProcessed(ctx context.Context, updateID int64) (bool, error)
}
type Model interface { Complete(ctx context.Context, req ai.Request) (ai.Message, error) }
type Tools interface {
    Definitions(callerID int64) []ai.ToolDefinition
    Execute(ctx context.Context, callerID int64, name, args string) string
}
type Options struct {
    Store Store; Model Model; Tools Tools
    SystemPrompt string
    MaxHistory, MaxToolCalls int
}
func New(Options) *Agent
func (a *Agent) Reply(ctx context.Context, userID, updateID int64, text string) (string, error)
var ErrDuplicate = errors.New("agent: update already processed")
func sanitize(history []ai.Message) []ai.Message
```

Loop: at most `MaxToolCalls+2` model requests per turn as a hard stop. Tool calls beyond the limit get `{"ok":false,"error":{"code":"tool_limit",…}}` and the next request sets `ForbidTools`. A final message with empty content returns `ai.ErrEmptyResponse`. A storage `ErrDuplicateUpdate` on save maps to `ErrDuplicate`.

- [ ] Tests first with a scripted fake model, a fake tools value and an in-memory store: plain reply saves `[user, assistant]` and sends system prompt + history + user; one tool round trip saves `[user, assistant(tool_calls), tool, assistant]` and the tool result reaches the second request with the right `tool_call_id`; a model that always asks for tools stops after `MaxToolCalls` executions, the following request has `ForbidTools`, and the turn ends; model error saves nothing; empty final content saves nothing and returns `ErrEmptyResponse` (Review Focus 3); already-processed update returns `ErrDuplicate` without calling the model; the tool executor receives the caller's user ID. `sanitize`: leading assistant/tool messages dropped; an assistant tool-call message missing one result is dropped with its partial results; an orphan tool message is dropped; a complete exchange is kept intact.
- [ ] Run — fails. Implement. Verify and commit.

### Task 7: Service

**Files:** `internal/service/{service.go,commands.go,limiter.go,texts.go,service_test.go,limiter_test.go}`

**Produces:**

```go
type Sender interface { SendMessage(ctx context.Context, chatID int64, text string) error }
type Replier interface { Reply(ctx context.Context, userID, updateID int64, text string) (string, error) }
type Resetter interface { Reset(ctx context.Context, userID int64) error }
type Options struct {
    Sender Sender; Replier Replier; Resetter Resetter
    Logger *slog.Logger
    Model string
    Workers, QueueSize, RatePerMinute, MaxInputChars int
    TurnTimeout time.Duration
    IsAdmin func(int64) bool
    AdminCommands map[string]bool
}
func New(Options) *Service
func (s *Service) Handle(u telegram.Update) // non-blocking enqueue
func (s *Service) Close()                   // close queues, wait for workers
func HelpText() string
```

Workers are started by `New`. Each job runs under `context.WithTimeout(context.Background(), TurnTimeout)` so shutdown lets in-flight turns finish. `limiter` is a mutex-guarded map of token buckets with an `allow(userID, now)` method and a `prune(now)` called from the worker path every N calls. User-facing strings live in `texts.go`; the bot's fixed messages are in English.

- [ ] Tests first with fake sender/replier/resetter: `/start`, `/help`, `/about`, `/model` reply with the expected text; `/model@mybot` works; `/reset` calls the resetter with the sender's ID; an unknown command points to `/help`; a command in `AdminCommands` is refused for a non-admin and allowed for an admin; plain text reaches the replier and its answer is sent; replier error → generic error text with no error detail; `ErrDuplicate` → nothing sent; over-long input → "too long" and replier not called; a photo (no text) → "text only"; whitespace-only text ignored; updates with nil message, nil `from`, `is_bot`, or a group chat are ignored (Review Focus 2); the 11th message in a minute at limit 10 → rate-limit text; with one worker blocked and `QueueSize` 1, a third update gets the busy text; two messages from one user are processed in order; `Close` waits for the in-flight job. Limiter: burst up to the limit, refill over time, separate users independent, prune removes idle entries.
- [ ] Run — fails. Implement. Verify and commit.

### Task 8: Wiring and deployment files

**Files:** `cmd/bot/main.go`, `Makefile`, `Dockerfile`, `docker-compose.yml`, `telegram-ai-agent.service`

`main` → `run(ctx, args) error`: load `.env` and config, build the logger, open the store. `bot migrate` applies migrations and exits. Otherwise: `CheckSchema`, build registry, AI client, agent, Telegram client and service; start the hourly retention loop when enabled; `Poll` until `signal.NotifyContext` (SIGINT, SIGTERM) fires; then `service.Close()` and `store.Close()`. The Telegram HTTP client timeout is the poll timeout plus 10s; the OpenRouter client relies on the turn context. Import `_ "time/tzdata"`.

- [ ] Implement. `go build ./...`.
- [ ] Smoke test against the throwaway database with fake tokens: `bot migrate` prints the applied count; a second run applies 0; starting the bot without migrating a fresh database exits with the schema error; starting with a fake token exits with a Telegram 401/404 error that does not print the token; a missing variable names it.
- [ ] `docker build .` succeeds; `docker compose config` validates.
- [ ] Verify and commit.

### Task 9: README, security pass, final verification

**Files:** `README.md`, spec dependency sentence.

- [ ] Write the README sections listed in the spec, for a reader new to Go deployment: architecture, configuration table, local setup, tests, choosing a tool-capable model, VPS deployment end to end, backup and restore, long-polling limits and switching to webhooks, security checklist, troubleshooting.
- [ ] Security pass over the diff: grep for logging of secrets and message text; confirm every SQL statement is parameterized and keyed by the update's user ID; confirm no `os/exec`; confirm response bodies are size-limited; confirm the admin gate cannot be reached through model output.
- [ ] Run `gofmt -l .`, `go vet ./...`, `go test -race ./...` with and without `TEST_db_POSTGRES_URL`; record the real output.
- [ ] Stop the throwaway database container. Commit.
