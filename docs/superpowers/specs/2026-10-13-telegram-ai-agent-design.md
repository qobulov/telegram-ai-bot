# Telegram AI Agent — Design

Date: 2026-10-13
Status: awaiting review

## Goal

A Telegram assistant bot in Go that answers private messages through OpenRouter,
remembers each user's conversation in PostgreSQL, and can call a small set of
safe tools. It must run unattended on an Ubuntu VPS under systemd, survive
restarts without losing history, and be testable without real credentials.

Success means: the binary builds, `go test -race ./...` and `go vet ./...` pass
without Telegram or OpenRouter credentials, and the README is enough for a
beginner to run it locally and deploy it.

## Decisions taken with the owner

- **Data access uses `pgx/v5` and PostgreSQL directly.** The workspace
  `CLAUDE.md` forbids direct database connections in favour of the Ucode SDK;
  this project is an explicit, owner-approved exception to that rule.
- **Tool calls are stored as `jsonb` on the assistant message**, not in a
  separate table.
- Work happens on a feature branch; nothing is pushed.

## Approach

Standard library first. `github.com/jackc/pgx/v5` is the only third-party
dependency. The Telegram client, OpenRouter client, `.env` loader, migration
runner and calculator parser are each small enough to write directly.

Rejected: a Telegram bot library, golang-migrate, a generic JSON Schema
validator and godotenv (each replaces under ~150 lines of code); a webhook
server (the brief asks for long polling, so webhooks are documented only).

## Packages

```text
cmd/bot/main.go          wiring, signals, `migrate` subcommand
internal/config          load .env + environment, validate
internal/telegram        Bot API client, polling, message splitting
internal/ai              OpenRouter client, types, retry
internal/tools           registry + get_current_time, calculator, get_bot_help
internal/storage         pgx pool, migrations, conversation queries
internal/storage/migrations/*.sql   embedded SQL migrations
internal/agent           one conversation turn, tool loop
internal/service         update dispatch, commands, rate limit, workers
```

Migrations live under `internal/storage/migrations` rather than a top-level
`migrations/` so they can be embedded with `go:embed` and shipped inside the
binary. There is no top-level `tests/` directory: tests sit beside the code
they test, which is the Go convention.

Dependency direction: `service → agent → {ai, tools, storage interface}`.
`agent` and `service` declare the small interfaces they consume; `storage`,
`ai` and `telegram` do not import each other, except that `storage` and `tools`
use the message and tool-definition types from `ai`. No package-level mutable state.

## Configuration

Loaded from the environment. A `.env` file in the working directory is read
first if present and never overrides variables that are already set.

| Variable | Default | Notes |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | required | |
| `OPENROUTER_API_KEY` | required | |
| `DATABASE_URL` | required | |
| `OPENROUTER_MODEL` | `openrouter/free` | |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | |
| `TELEGRAM_API_URL` | `https://api.telegram.org` | |
| `SYSTEM_PROMPT` | built-in prompt | |
| `MAX_HISTORY_MESSAGES` | 20 | |
| `MAX_TOOL_CALLS_PER_TURN` | 4 | |
| `MAX_CONCURRENT_REQUESTS` | 10 | worker count |
| `USER_RATE_LIMIT_PER_MINUTE` | 10 | |
| `REQUEST_TIMEOUT_SECONDS` | 45 | budget for a whole turn |
| `MAX_OUTPUT_TOKENS` | 1024 | |
| `MAX_INPUT_CHARS` | 4000 | longer messages are rejected |
| `RETENTION_DAYS` | 90 | 0 disables cleanup |
| `LOG_LEVEL` | `info` | debug, info, warn, error |
| `ADMIN_USER_IDS` | empty | comma-separated Telegram IDs |
| `ADMIN_COMMANDS` | empty | comma-separated commands restricted to admins |

Validation fails startup with a message naming the variable, never its value.
Numeric settings must be positive (except `RETENTION_DAYS`, which may be 0).

## Database

```sql
users(
  telegram_id  bigint primary key,
  created_at   timestamptz not null default now(),
  last_seen_at timestamptz not null default now()
)

conversations(
  id         bigserial primary key,
  user_id    bigint not null unique references users on delete cascade,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
)

messages(
  id              bigserial primary key,
  conversation_id bigint not null references conversations on delete cascade,
  role            text not null check (role in ('user','assistant','tool')),
  content         text not null default '',
  tool_calls      jsonb,       -- assistant only
  tool_call_id    text,        -- tool only
  update_id       bigint,      -- Telegram update that produced a user message
  created_at      timestamptz not null default now(),
  check (tool_calls is null or role = 'assistant'),
  check ((role = 'tool') = (tool_call_id is not null))
)
```

Indexes: `messages(conversation_id, id)`, `conversations(updated_at)`, and a
unique partial index on `messages(update_id) where update_id is not null`.

- A user has at most one conversation. `/reset` deletes it in a transaction;
  messages go with it by cascade.
- `users` stores only the Telegram ID and timestamps. No names or usernames.
- Every query that touches a conversation is keyed by the Telegram user ID
  taken from the update, never by an ID supplied in message text.
- All SQL is parameterized.

**Migrations.** Numbered `.sql` files are embedded in the binary. `bot migrate`
takes a PostgreSQL advisory lock, creates `schema_migrations(version,
applied_at)` if missing, and applies each unapplied file in its own
transaction. The bot does not migrate on startup; it checks that the schema
version matches the newest embedded migration and exits with a clear error if
not.

**Retention.** When `RETENTION_DAYS > 0`, an hourly job deletes conversations
whose `updated_at` is older than that many days.

## A turn, end to end

1. `telegram` long-polls `getUpdates` (timeout 30s, `allowed_updates:
   ["message"]`). Network and 5xx errors back off exponentially with jitter up
   to 30s; a 429 waits for `retry_after`. A 401 is fatal.
2. `service` drops updates that are not a private-chat message from a human
   sender. Non-text messages get a short "text only" reply.
3. The update is routed to worker `user_id mod N`. Each worker has a bounded
   queue. If the queue is full the user is told the bot is busy and the update
   is dropped. This gives per-user ordering, bounded concurrency and
   backpressure. Trade-off: a slow turn delays other users on the same worker.
4. The worker applies the per-user rate limit (in-memory token bucket, idle
   entries pruned periodically), then handles a command or runs a turn.
5. `agent` checks the update ID has not been processed, loads the last
   `MAX_HISTORY_MESSAGES` messages, sanitizes them, and calls the model with
   the system prompt and tool definitions.
6. While the model asks for tools, each call is run through the registry and
   its result appended. `MAX_TOOL_CALLS_PER_TURN` counts calls across the whole
   turn. Once the limit is reached, remaining calls in that response get a
   structured "limit reached" result and the next request is sent with
   `tool_choice: "none"`, which forces a text answer and ends the loop.
7. The user message, every assistant and tool message, and the final answer
   are saved in one transaction. A failed turn saves nothing, so a partial
   tool sequence is never persisted.
8. The reply is sent as plain text (no `parse_mode`), split into chunks under
   Telegram's 4096 UTF-16 unit limit, preferring line breaks.

**History sanitizing.** Trimming by count can cut a tool exchange in half, so
after loading: drop leading messages until the first is a user message, and
drop any assistant message with tool calls (plus its results) unless every
call has a matching tool message directly after it. Orphan tool messages are
dropped.

**Duplicates.** The polling offset advances in memory as updates are received.
After a crash Telegram redelivers unconfirmed updates; those whose turn was
saved are recognised by `update_id` and skipped. Commands are idempotent and
are not deduplicated.

## OpenRouter client

`POST {base}/chat/completions` with bearer auth, model, messages, `max_tokens`
and, when enabled, `tools`. Standard `tool_calls` / `tool_call_id` message
shapes.

- Retries: up to 3 attempts for 429, 5xx and network errors, exponential
  backoff with jitter, honouring `Retry-After`. Other 4xx are not retried.
  Retries stop when the turn's context expires.
- Errors are typed (status, provider message) so callers can distinguish
  them. OpenRouter also reports some failures as an `error` object inside a
  200 response; that is treated as an error too.
- If a request with tools is rejected because the model has no tool support
  (404 or 400 whose message mentions tool use), the client retries once
  without tools and stays in no-tools mode until restart, logging a warning.
- An empty `choices` array or an empty final message is an error, not a blank
  reply.
- The API key is never logged. Response bodies are size-limited when read.

## Tools

```go
type Tool struct {
    Name        string
    Description string
    Schema      json.RawMessage // JSON Schema sent to the model
    AdminOnly   bool
    Timeout     time.Duration   // default 5s
    Run         func(ctx context.Context, args json.RawMessage) (any, error)
}
```

`Registry.Execute(ctx, caller, name, rawArgs)`:

- unknown name → structured error;
- `AdminOnly` and caller not in `ADMIN_USER_IDS` → structured error. This
  check uses the Telegram ID from the update, so nothing the model or the user
  writes can grant permission;
- runs with the tool's timeout; output larger than 4000 bytes is replaced by
  an error;
- always returns a JSON string: `{"ok":true,"result":…}` or
  `{"ok":false,"error":{"code":…,"message":…}}`.

Each tool decodes its arguments into a typed struct with unknown fields
rejected and then validates the values. Arguments that are not valid JSON
yield an `invalid_arguments` error the model can react to.

- `get_current_time` — optional `timezone`; validated with
  `time.LoadLocation`; rejects unknown names. The binary embeds `time/tzdata`
  so this works on minimal hosts.
- `calculator` — `expression`; a recursive-descent parser over `+ - * / ( )`,
  unary minus and decimal numbers. Limits on length and nesting depth. Rejects
  division by zero and non-finite results.
- `get_bot_help` — returns the command list and capabilities.

All three are available to every user. None has side effects, so the model
re-requesting one is harmless.

## Commands

`/start`, `/help`, `/reset`, `/model`, `/about`. A `@botname` suffix is
accepted. Unknown commands get a pointer to `/help`. Any command listed in
`ADMIN_COMMANDS` is refused for non-admins.

## Errors shown to users

Users see short fixed messages: busy, rate limited, message too long, text
only, and a generic "something went wrong". Provider messages, SQL errors and
stack details go to the log only. Logs use `slog` with the user ID and update
ID, and never include tokens, the API key, the database URL or message text.

## Shutdown

SIGINT or SIGTERM cancels polling and closes the worker queues. Workers finish
the turn in progress, bounded by the turn timeout, then the pool is closed.

## Testing

- `config`: required variables, defaults, bad numbers, secrets absent from
  error text.
- `ai`: success, tool-call response, 429 with `Retry-After`, 5xx retry then
  success, non-retryable 4xx, retry limit, context cancellation, in-body
  error, no-tool-support fallback — all against `httptest`.
- `telegram`: `getUpdates` parsing and offset, `sendMessage`, 429 handling,
  API error decoding, message splitting including multi-byte text.
- `tools`: registry (unknown tool, admin gate, timeout, oversized output,
  malformed JSON, unknown properties), calculator table tests, timezone
  validation.
- `agent`: plain reply, tool round trip, tool-call limit, history sanitizing,
  duplicate update, model error saves nothing — with an in-memory store and a
  fake model.
- `service`: commands, admin restriction, rate limiting, queue-full
  backpressure, unsupported message types, per-user ordering.
- `storage`: integration tests for history, reset, cross-user isolation,
  duplicate update IDs, retention and migrations. They run only when
  `TEST_DATABASE_URL` is set and skip otherwise.

## Deployment and docs

- `Makefile`: build, test, race, vet, fmt, migrate.
- `Dockerfile` (multi-stage, non-root) and `docker-compose.yml` with a
  PostgreSQL service that publishes no ports.
- `telegram-ai-agent.service`: dedicated user, `EnvironmentFile` with mode
  0600, `Restart=on-failure`, systemd hardening options.
- `README.md`: architecture, local setup, VPS deployment (Go, PostgreSQL,
  restricted role, migrations, service user, firewall, logs, safe updates),
  `pg_dump` backup and restore, choosing a tool-capable model, long-polling
  limits and how to move to webhooks, security checklist, troubleshooting.

Two database roles are documented: an owner role used only for `bot migrate`,
and a runtime role with `SELECT, INSERT, UPDATE, DELETE` on the three tables,
`USAGE` on their sequences, and `SELECT` on `schema_migrations` for the
startup version check.

## Out of scope

Webhook server, group chats, media messages, streaming replies, metrics
endpoints, multiple conversations per user, and any tool with side effects.
