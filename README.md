# telegram-ai-bot

A Telegram AI assistant written in Go. It answers private messages, and
group messages that address it, through [OpenRouter](https://openrouter.ai).
It remembers each user's conversation in PostgreSQL and can call a small set
of safe tools (current time, calculator, bot help).

- Long polling: no public port, domain or TLS certificate needed.
- One dependency besides the Go standard library: `pgx` for PostgreSQL.
- In groups it answers only when mentioned or replied to, as a reply to the
  message that addressed it.
- Replies in the user's language (Uzbek, Russian, English).
- Tests run without Telegram or OpenRouter credentials.

## Contents

1. [Architecture](#architecture)
2. [Configuration](#configuration)
3. [Local setup](#local-setup)
4. [Tests](#tests)
5. [Choosing a model](#choosing-a-model)
6. [Groups](#groups)
7. [VPS deployment (systemd)](#vps-deployment-systemd)
8. [Docker Compose deployment (optional)](#docker-compose-deployment-optional)
9. [Backup and restore](#backup-and-restore)
10. [Long polling and webhooks](#long-polling-and-webhooks)
11. [Security checklist](#security-checklist)
12. [Troubleshooting](#troubleshooting)

## Architecture

```text
cmd/bot/main.go                 wiring, signals, "migrate" subcommand
internal/config                 load .env + environment, validate
internal/telegram               Bot API client: long polling, sending, message splitting
internal/ai                     OpenRouter client: types, retries, tool-calling format
internal/tools                  tool registry + get_current_time, calculator, get_bot_help
internal/storage                pgx pool, migrations, conversation queries
internal/storage/migrations     SQL migrations, embedded in the binary
internal/agent                  one conversation turn and the tool-calling loop
internal/service                update dispatch: commands, rate limit, worker pool
```

How one message travels through the bot:

1. `telegram` long-polls `getUpdates` and passes each update to `service`.
2. `service` keeps messages from a person that are either in a private chat
   or, in a group, addressed to the bot (see [Groups](#groups)). Everything
   else is dropped. A kept update goes on the queue of the worker that owns
   that user.
3. The worker applies the per-user rate limit and, in a group, the per-group
   limit, then runs a command or calls `agent`.
4. `agent` loads the user's recent history, sends it to the model with the
   tool definitions, runs any tools the model asks for, and repeats until the
   model answers in text.
5. The whole turn (question, tool calls, tool results, answer) is saved in one
   database transaction, and the answer is sent back.

Design points worth knowing:

- **Workers.** There are `MAX_CONCURRENT_REQUESTS` workers. A user is always
  served by the same worker, so their messages are handled in order, one at a
  time. Each worker has a queue of 8 updates; when it is full the user gets a
  "busy" reply. A slow answer delays other users who share that worker.
- **Tool safety.** Only tools registered in code can run. Arguments are
  decoded into typed structs, unknown properties are rejected, each run has a
  timeout and an output size limit. Permission to use a tool depends only on
  the Telegram user ID of the sender, never on anything the model or the user
  writes. The calculator is a hand-written parser; nothing is ever passed to a
  shell or an interpreter.
- **Tool loop limit.** `MAX_TOOL_CALLS_PER_TURN` counts calls across one
  message. After the limit, further calls are refused, the model is told to
  answer without tools, and the turn fails if it still does not.
- **History integrity.** History is trimmed by message count, which could cut
  a tool exchange in half. Before each request the history is cleaned so it
  starts at a user message and contains no tool call without all its results.
- **Duplicates.** Each user message stores its Telegram `update_id` under a
  unique index. An update redelivered after a crash is recognised and skipped.

### Database schema

```text
users(telegram_id PK, created_at, last_seen_at)
conversations(id PK, chat_id, user_id -> users, created_at, updated_at,
              UNIQUE (chat_id, user_id))
messages(id PK, conversation_id -> conversations, role, content,
         tool_calls jsonb, tool_call_id, update_id,
         telegram_message_id, reply_to_message_id, created_at)
schema_migrations(version PK, applied_at)
```

A conversation belongs to one user in one chat. A user's private chat (where
the chat ID equals the user ID) and their conversation in each group are
separate rows, so they are never loaded together. `/reset` deletes the
sender's conversation in the chat where it was sent; its messages go with it
through `ON DELETE CASCADE`.

`telegram_message_id` and `reply_to_message_id` record which Telegram message
a row corresponds to and which one it replied to. Only the numeric Telegram
ID is stored about a user: no name, username or phone number. Message text is
stored, because that is the conversation memory. The schema is defined by the
files in [internal/storage/migrations/](internal/storage/migrations/).

## Configuration

Settings come from environment variables. For local development, put them in
a `.env` file in the directory you start the bot from; variables already set
in the environment win over the file.

| Variable | Default | Meaning |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | required | Token from [@BotFather](https://t.me/BotFather) |
| `OPENROUTER_API_KEY` | required | Key from <https://openrouter.ai/keys> |
| `DATABASE_URL` | required | PostgreSQL connection string |
| `OPENROUTER_MODEL` | `openrouter/free` | Model ID, see [Choosing a model](#choosing-a-model) |
| `MAX_OUTPUT_TOKENS` | `1024` | Longest model reply, in tokens |
| `SYSTEM_PROMPT` | built-in | Instructions sent to the model on every request |
| `MAX_HISTORY_MESSAGES` | `20` | Recent messages sent to the model as context |
| `MAX_TOOL_CALLS_PER_TURN` | `4` | Tool calls allowed while answering one message |
| `MAX_CONCURRENT_REQUESTS` | `10` | Number of workers |
| `USER_RATE_LIMIT_PER_MINUTE` | `10` | Messages each user may send per minute |
| `REQUEST_TIMEOUT_SECONDS` | `45` | Time budget for answering one message |
| `MAX_INPUT_CHARS` | `4000` | Longest user message accepted |
| `RETENTION_DAYS` | `90` | Delete conversations idle longer than this; `0` keeps them forever |
| `ADMIN_USER_IDS` | empty | Comma-separated Telegram user IDs of administrators |
| `ADMIN_COMMANDS` | empty | Comma-separated commands only administrators may use, e.g. `model,about` |
| `ENABLE_MENTION_TRIGGER` | `true` | In groups, answer when `@username` is mentioned |
| `ENABLE_REPLY_TRIGGER` | `true` | In groups, answer replies to the bot's messages |
| `ENABLE_DISPLAY_NAME_TRIGGER` | `false` | In groups, answer messages that start with the bot's name; see [Groups](#groups) |
| `BOT_DISPLAY_NAME` | empty | The name for that trigger, e.g. `Mira`; required when it is enabled |
| `GROUP_CONTEXT_MODE` | `per_user` | Only `per_user` is supported |
| `MAX_GROUP_REPLY_LENGTH` | `4000` | Longest answer posted in a group, in characters (at most 4096) |
| `GROUP_RATE_LIMIT_PER_MINUTE` | `10` | Messages answered per group per minute, across all members |
| `GROUP_SYSTEM_PROMPT` | built-in | Instructions sent to the model for group messages |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | Override for testing or a proxy |
| `TELEGRAM_API_URL` | `https://api.telegram.org` | Override for testing or a local Bot API server |

The bot refuses to start when a required variable is missing or a number is
invalid, and says which variable is wrong. It never prints a variable's value.

To find your Telegram user ID for `ADMIN_USER_IDS`, send a message to the bot
and look for `user_id=` in the log.

The pool size can be set in the connection string, for example
`...?sslmode=disable&pool_max_conns=10`. The default is the larger of 4 and
the number of CPUs.

## Local setup

You need Go 1.27 or newer and PostgreSQL 14 or newer.

```bash
# 1. Create a database
createdb tgbot

# 2. Configure
cp .env.example .env
# Edit .env: set TELEGRAM_BOT_TOKEN, OPENROUTER_API_KEY and, for example,
# DATABASE_URL=postgres://localhost:5432/tgbot?sslmode=disable

# 3. Create the tables
make migrate

# 4. Start the bot
make run
```

Send `/start` to your bot in Telegram. Stop the bot with Ctrl+C; it finishes
the message it is working on before exiting.

Bot commands: `/start`, `/help`, `/reset` (delete your history), `/model`,
`/about`.

## Tests

```bash
go test ./...          # all tests
go test -race ./...    # with the race detector
go vet ./...
gofmt -w .
make check             # vet + race tests + formatting check
```

No test talks to Telegram or OpenRouter; both are replaced by local fake
servers.

The storage tests and the end-to-end test need a real PostgreSQL database and
are skipped unless `TEST_DATABASE_URL` is set:

```bash
createdb tgbot_test
TEST_DATABASE_URL='postgres://localhost:5432/tgbot_test?sslmode=disable' go test -p 1 ./...
```

**Use a disposable database.** These tests drop and recreate the bot's
tables. `-p 1` runs packages one after another so they do not share the
database at the same time.

## Choosing a model

`OPENROUTER_MODEL` accepts any model ID from <https://openrouter.ai/models>.

Tools (time, calculator, help) only work with models that support tool
calling. To list those, filter the catalogue by the "tools" supported
parameter: <https://openrouter.ai/models?supported_parameters=tools>. Add
`max_price=0` to the filter to see only free ones.

The default, `openrouter/free`, lets OpenRouter pick a free model for each
request. That is convenient for trying the bot, but the chosen model changes
and may not support tools. For predictable behaviour set a specific model ID.

If the model rejects a request because it cannot use tools, the bot logs
`model does not support tool calling; continuing without tools`, answers
without tools, and stays in that mode until it is restarted. In that mode the
model may guess at the time or at arithmetic.

Free models are rate limited by OpenRouter. The bot retries a rate-limited
request up to three times and then tells the user something went wrong.

## Groups

Add the bot to a group or supergroup like any other member. It then answers
only messages that address it, and posts each answer as a reply to the
message that asked.

### Ways to address the bot

| How | Example | Setting |
|---|---|---|
| Mention its username | `@MyAssistantBot explain the difference between Go and Python` | `ENABLE_MENTION_TRIGGER` (on) |
| Reply to one of its messages | reply to the bot's answer with `and which is faster?` | `ENABLE_REPLY_TRIGGER` (on) |
| A command with its username | `/reset@MyAssistantBot` | always on |
| Start the message with its name | `Mira, explain this code.` | `ENABLE_DISPLAY_NAME_TRIGGER` (off) |

The username is the bot's real one, read from Telegram at startup; it is not
configured. Mentions are found through Telegram's message entities, so
`@MyAssistantBot2` or the name inside another word does not trigger the bot.

A plain `/reset` or `/help` without the bot's username is ignored in a group,
because it may be meant for another bot.

The display-name trigger requires the name to open the message and be
followed by a comma or colon. "I think Mira is wrong" and "Miranda, hi" do
not trigger it.

### Example

```text
Ann:   Go is interpreted, right?
Bek:   (replying to Ann)  @MyAssistantBot is that right?
Bot:   (replying to Bek)  No. Go is compiled ahead of time to machine code...
Bek:   (replying to Bot)  and Python?
Bot:   (replying to Bek)  Python is usually interpreted...
Ann:   what a nice day
       (no reaction: not addressed to the bot)
```

### What the model sees

- The text of the message that addressed the bot, with the mention removed.
- If that message is a reply, the one message it replies to, shortened to
  1,000 characters and marked as a quote. A quote from another member is
  labelled as untrusted content.
- The asking member's own earlier exchanges with the bot **in that group**.

Nothing else from the group is read, stored or sent to the model. Each member
has their own conversation with the bot in each group, separate from every
other member's and from their private chat with the bot. `/reset@YourBot`
clears only the sender's conversation in that group.

Tools work in groups as in private chats. Administrator-only tools and
commands depend on who sent the message, not on the group.

### Limits

- Group answers are cut to `MAX_GROUP_REPLY_LENGTH` characters so the bot
  posts one message. Ask in a private chat for long answers.
- `GROUP_RATE_LIMIT_PER_MINUTE` caps answers per group across all members;
  the per-user limit applies as well. The first refusal is announced, later
  ones are silent.
- The bot reads text. A photo or document counts only through its caption;
  the image itself is not seen.
- Messages from other bots are ignored. So are messages from anonymous group
  administrators, which Telegram delivers as coming from a bot account.

### BotFather privacy mode

Telegram bots have *privacy mode*, which is **on** by default. With it on,
Telegram delivers to a bot in a group only:

- messages that mention the bot by `@username`,
- replies to the bot's own messages,
- commands addressed to it (`/command@YourBot`).

That is exactly what the mention, reply and command triggers need, so
**leave privacy mode on**. The bot then never receives the rest of the
group's conversation at all.

Only the display-name trigger needs more: "Mira, explain this" contains no
mention, so Telegram does not deliver it with privacy mode on. To use that
trigger:

1. In [@BotFather](https://t.me/BotFather): `/setprivacy`, choose the bot,
   choose **Disable**.
2. Remove the bot from the group and add it again. Telegram applies the
   setting when the bot joins.
3. Set `ENABLE_DISPLAY_NAME_TRIGGER=true` and `BOT_DISPLAY_NAME=...`, and
   restart the bot.

With privacy mode off, Telegram sends the bot every message in the group. The
bot still ignores, and does not store, anything that does not address it, but
group members should be told the bot now receives their messages. (A bot that
is a group administrator also receives all messages, whatever the setting.)

To check the setting: `/mybots` in @BotFather, choose the bot, then *Bot
Settings* and *Group Privacy*. To stop the bot from being added to groups at
all, use `/setjoingroups`.

## VPS deployment (systemd)

These steps are for Ubuntu 22.04 or 24.04, run as a user with `sudo`.

### 1. Build the binary

The simplest way is to build on your own machine and copy one file to the
server. Go cross-compiles without extra tools:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o telegram-ai-agent ./cmd/bot
scp telegram-ai-agent telegram-ai-agent.service you@your-server:/tmp/
```

Use `GOARCH=arm64` for an ARM server. To build on the server instead, install
Go there first:

```bash
curl -LO https://go.dev/dl/go1.27.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
go version
```

Ubuntu's own `golang` package is usually too old for this project.

### 2. Install PostgreSQL

```bash
sudo apt update
sudo apt install -y postgresql
sudo systemctl enable --now postgresql
```

PostgreSQL on Ubuntu listens only on `localhost` by default. Leave it that
way: the bot runs on the same machine.

### 3. Create the database and two roles

The bot uses two roles so that the running bot cannot change the schema:

- `tgbot_owner` owns the database and is used only to apply migrations.
- `tgbot` is used by the running bot and can only read and write rows.

Generate two passwords (`openssl rand -hex 24`) and run:

```bash
sudo -u postgres psql <<'SQL'
CREATE ROLE tgbot_owner LOGIN PASSWORD 'OWNER_PASSWORD_HERE';
CREATE ROLE tgbot       LOGIN PASSWORD 'RUNTIME_PASSWORD_HERE';
CREATE DATABASE tgbot OWNER tgbot_owner;
REVOKE ALL ON DATABASE tgbot FROM PUBLIC;
GRANT CONNECT ON DATABASE tgbot TO tgbot;

\connect tgbot
-- Every table tgbot_owner creates from now on is usable by tgbot.
ALTER DEFAULT PRIVILEGES FOR ROLE tgbot_owner IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tgbot;
ALTER DEFAULT PRIVILEGES FOR ROLE tgbot_owner IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO tgbot;
SQL
```

### 4. Install the binary and a service user

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin tgbot
sudo install -o root -g root -m 0755 /tmp/telegram-ai-agent /usr/local/bin/telegram-ai-agent
```

### 5. Write the protected configuration

```bash
sudo install -d -o root -g tgbot -m 0750 /etc/telegram-ai-agent
sudo install -o root -g tgbot -m 0640 /dev/null /etc/telegram-ai-agent/env
sudo nano /etc/telegram-ai-agent/env
```

Put one `KEY=value` per line, without quotes:

```dotenv
TELEGRAM_BOT_TOKEN=123456:your-token
OPENROUTER_API_KEY=sk-or-your-key
OPENROUTER_MODEL=openrouter/free
DATABASE_URL=postgres://tgbot:RUNTIME_PASSWORD_HERE@127.0.0.1:5432/tgbot?sslmode=disable
LOG_LEVEL=info
```

Only root can edit this file, and only the `tgbot` user can read it.
`sslmode=disable` is fine here because the connection never leaves the
machine. A systemd environment file cannot hold a multi-line value; keep
`SYSTEM_PROMPT` on one line or leave it out to use the built-in prompt.

### 6. Apply migrations

Migrations run as the owner role. The `migrate` command needs only
`DATABASE_URL`, not the bot's other secrets:

```bash
sudo -u tgbot env \
  DATABASE_URL='postgres://tgbot_owner:OWNER_PASSWORD_HERE@127.0.0.1:5432/tgbot?sslmode=disable' \
  /usr/local/bin/telegram-ai-agent migrate

# The running bot only needs to read the migration history.
sudo -u postgres psql -d tgbot -c \
  'REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM tgbot'
```

You should see `migrations finished applied=2` on a new database. Running it
again prints
`applied=0`: applied migrations are recorded in `schema_migrations` and never
run twice. The bot does not migrate on its own; if the schema is behind it
exits with `database schema is out of date; run the migrate command`.

### 7. Install and start the service

```bash
sudo install -o root -g root -m 0644 /tmp/telegram-ai-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now telegram-ai-agent
```

The unit restarts the bot if it crashes (`Restart=on-failure`) and starts it
at boot.

### 8. Firewall

The bot only makes outgoing connections, so no port needs to be opened for
it. Allow SSH and nothing else:

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw enable
sudo ufw status
```

Do not open port 5432.

### 9. Status and logs

```bash
systemctl status telegram-ai-agent
journalctl -u telegram-ai-agent -f            # follow
journalctl -u telegram-ai-agent --since today
```

A healthy start logs `bot started` with the bot's username. Each answered message logs
`turn completed` with the user ID and duration. Message text is never logged.

### 10. Updating safely

```bash
# On your machine: build the new version and copy it over, as in step 1.

# On the server:
sudo -u postgres pg_dump -Fc tgbot > ~/tgbot-before-update.dump   # backup first
sudo systemctl stop telegram-ai-agent
sudo cp /usr/local/bin/telegram-ai-agent /usr/local/bin/telegram-ai-agent.previous
sudo install -o root -g root -m 0755 /tmp/telegram-ai-agent /usr/local/bin/telegram-ai-agent
# Apply migrations: repeat the first command of step 6. Upgrading from a
# version without group support applies migration 0002, which keeps all
# existing private conversations.
sudo systemctl start telegram-ai-agent
journalctl -u telegram-ai-agent -n 20
```

Stopping lets the message each worker is answering finish; anyone still
waiting in a queue is told the bot is busy. Messages sent while the
bot is down are not lost: Telegram keeps them for up to 24 hours and delivers
them when the bot starts again.

To roll back, stop the service, copy `telegram-ai-agent.previous` back and
start it. If the new version applied a migration, also restore the backup.

## Docker Compose deployment (optional)

```bash
cp .env.example .env            # set TELEGRAM_BOT_TOKEN and OPENROUTER_API_KEY
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
docker compose up -d --build
docker compose logs -f bot
```

Compose starts PostgreSQL, applies migrations, then starts the bot.
`DATABASE_URL` is set by the compose file, so the value in `.env` is ignored.
The database publishes no ports. This setup uses a single database role for
simplicity; prefer the systemd deployment when you want the restricted
runtime role.

```bash
docker compose exec -T db pg_dump -U tgbot -Fc tgbot > tgbot.dump  # backup
docker compose pull && docker compose up -d --build                # update
```

## Backup and restore

Back up:

```bash
sudo -u postgres pg_dump -Fc tgbot > tgbot-$(date +%F).dump
```

Nightly backup at 03:00, keeping 14 days, as a root cron job
(`sudo crontab -e`):

```cron
0 3 * * * install -d -m 0700 /var/backups/tgbot && sudo -u postgres pg_dump -Fc tgbot > /var/backups/tgbot/tgbot-$(date +\%F).dump && find /var/backups/tgbot -name '*.dump' -mtime +14 -delete
```

Copy backups off the server; a backup on the same disk does not survive
losing the disk. Dumps contain users' messages, so protect them like the
database.

Restore:

```bash
sudo systemctl stop telegram-ai-agent
sudo -u postgres pg_restore --clean --if-exists -d tgbot < tgbot-2026-01-31.dump
sudo systemctl start telegram-ai-agent
```

Test a restore into a scratch database once, before you need it:

```bash
sudo -u postgres createdb tgbot_restore_test
sudo -u postgres pg_restore -d tgbot_restore_test < tgbot-2026-01-31.dump
sudo -u postgres psql -d tgbot_restore_test -c 'SELECT count(*) FROM messages'
sudo -u postgres dropdb tgbot_restore_test
```

**Retention.** Conversations idle for more than `RETENTION_DAYS` are deleted
by an hourly job. A user can delete their own history at any time with
`/reset`. Backups keep deleted data until they are rotated out.

## Long polling and webhooks

The bot uses long polling: it asks Telegram for new messages and waits up to
30 seconds for an answer. Limitations:

- **One instance per token.** Two processes polling with the same token
  receive `409 Conflict` and steal updates from each other. You cannot scale
  out by starting a second copy.
- **In-memory limits.** Rate-limit counters and worker queues live in the
  process. They reset on restart.
- **Missed messages expire.** Telegram keeps undelivered updates for 24
  hours. A longer outage loses older messages.
- **Polling and a webhook cannot be combined.** If a webhook is set for the
  token, polling fails with `409 Conflict` until it is removed.

Polling has one real advantage: the server needs no public address, domain or
certificate, and no inbound port.

Switching to webhooks is worth it when you need several instances behind a
load balancer. It is not implemented here; the outline is:

1. Give the server a public HTTPS address (a domain and a reverse proxy such
   as Caddy or nginx with a certificate). Telegram accepts ports 443, 80, 88
   and 8443.
2. Add an HTTP handler that reads the JSON update from the request body,
   decodes it into `telegram.Update`, and calls `service.Handle`. Reply
   `200 OK` immediately; the answer is sent later by a worker, as now.
3. Reject requests whose `X-Telegram-Bot-Api-Secret-Token` header does not
   match a secret you choose. Without this check anyone who finds the URL can
   forge messages from any user ID.
4. Register it:
   `curl "https://api.telegram.org/bot$TOKEN/setWebhook" -d url=https://your.domain/webhook -d secret_token=$SECRET -d 'allowed_updates=["message"]'`
5. Stop calling `Poll`. With several instances, move the rate limiter and the
   per-user ordering to shared storage, because they are per-process today.

To go back to polling: `curl "https://api.telegram.org/bot$TOKEN/deleteWebhook"`.

## Security checklist

Built in:

- [x] Secrets come only from the environment; `.env` is git-ignored.
- [x] Tokens, API keys, the database URL and message text are never logged.
      The bot token is stripped from network errors before they are logged.
- [x] Users see fixed, generic error messages; details go to the log.
- [x] All SQL is parameterized. The model cannot produce SQL that is run.
- [x] Every query on a conversation is keyed by the sender's Telegram ID
      taken from the update, never by an ID in message text.
- [x] Only messages from human senders are accepted: in private chats, and in
      groups when they address the bot. The bot's identity for that check
      comes from Telegram (`getMe`), not from configuration.
- [x] Private and group conversations are stored under separate keys and are
      never loaded together; group members never see each other's history.
- [x] A message quoted from another group member is marked as untrusted and
      shortened before it reaches the model.
- [x] Input length, output tokens, tool output size, tool run time, tool
      calls per message and time per message are all limited.
- [x] Per-user rate limit; fixed number of workers; bounded queues.
- [x] Only registered tools run. No shell, no `eval`, no model-written SQL.
- [x] Tool permissions and restricted commands are checked against
      `ADMIN_USER_IDS` in code. A prompt cannot grant them.
- [x] The system prompt tells the model to treat messages and tool results as
      untrusted, but nothing relies on the model obeying it.

For you to do when deploying:

- [ ] Use the restricted `tgbot` role for the running bot.
- [ ] Keep the environment file at mode `0640`, owned by `root:tgbot`.
- [ ] Keep PostgreSQL on `localhost`; do not open port 5432.
- [ ] Enable the firewall with only SSH allowed in.
- [ ] Use SSH keys and disable password login.
- [ ] Set a spending limit on the OpenRouter key.
- [ ] Schedule backups, copy them off the server, and test a restore.
- [ ] Keep Ubuntu, PostgreSQL and Go dependencies updated
      (`go list -m -u all`, `govulncheck ./...`).
- [ ] If a token leaks, revoke it in @BotFather (`/revoke`) or on
      openrouter.ai and update the environment file.

Known limits:

- Conversation text is stored unencrypted in PostgreSQL. Use disk encryption
  if that matters for your users.
- Message text is sent to OpenRouter and the model provider behind it. Tell
  your users, and check the provider's data policy for the model you choose.
- In a group, anyone who can post can make the bot answer, within the rate
  limits. The answer is visible to the whole group.
- A model can still be talked into saying untrue or unwanted things. The bot
  limits what it can *do* (three harmless tools), not what it can *say*.

## Troubleshooting

| Symptom | Likely cause and fix |
|---|---|
| `invalid configuration: X is required` | The variable is missing. Check `.env` or `/etc/telegram-ai-agent/env`. |
| `database schema is out of date; run the migrate command` | Run `make migrate` locally, or step 6 on the server. |
| `database is unreachable` | PostgreSQL is not running, or host, port, user or password in `DATABASE_URL` is wrong. Try `psql "$DATABASE_URL"`. |
| `permission denied for table ...` | The runtime role lacks grants. Run the `ALTER DEFAULT PRIVILEGES` statements from step 3 *before* migrating, or grant on the existing tables: `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO tgbot; GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO tgbot;` |
| `telegram polling failed ... 409 Conflict` | Another copy of the bot is running with the same token, or a webhook is set. Stop the other copy, or call `deleteWebhook`. |
| The bot is silent in a group | It answers only when addressed: mention `@username`, reply to one of its messages, or use `/command@username`. Check `ENABLE_MENTION_TRIGGER` and `ENABLE_REPLY_TRIGGER`. If the sender is an anonymous admin or another bot, it is ignored by design. |
| The bot ignores `/reset` in a group | Commands in groups need the bot's username: `/reset@YourBot`. |
| "Mira, ..." gets no answer | The display-name trigger is off by default, and needs BotFather privacy mode disabled and the bot re-added to the group; see [Groups](#groups). The name must start the message and be followed by `,` or `:`. |
| The bot answers but not as a reply | The original message was deleted before the answer arrived; the answer is then sent as an ordinary message. |
| Group answers end in `…` | They were cut to `MAX_GROUP_REPLY_LENGTH`. |
| "This group is sending me messages too quickly" | The per-group limit. Raise `GROUP_RATE_LIMIT_PER_MINUTE`. |
| `identify the bot: telegram: 401 Unauthorized` at startup | The bot token is wrong or was revoked. |
| `BOT_DISPLAY_NAME is required when ENABLE_DISPLAY_NAME_TRIGGER is true` | Set the name or turn the trigger off. |
| "Something went wrong on my side" | Look at the log line `turn failed`. `status 401`: bad OpenRouter key. `status 402`: no credits. `status 429`: the free model is rate limited; wait or pick another model. `status 404`: the model ID does not exist. |
| "That took too long to answer" | The model was slower than `REQUEST_TIMEOUT_SECONDS`. Raise it or use a faster model. |
| Wrong time or wrong arithmetic | The model has no tool support and is guessing. Look for `model does not support tool calling` in the log and see [Choosing a model](#choosing-a-model). |
| "You're sending messages too quickly" | Per-user rate limit. Raise `USER_RATE_LIMIT_PER_MINUTE` if it is too strict. |
| "I'm handling a lot of messages right now" | Worker queues are full. Raise `MAX_CONCURRENT_REQUESTS`, and `pool_max_conns` to match. |
| The bot forgot the conversation | History is limited to `MAX_HISTORY_MESSAGES`, and idle conversations are deleted after `RETENTION_DAYS`. |
| Service will not start under systemd | `journalctl -u telegram-ai-agent -n 50`. Check the environment file's owner and mode, and that it has no quotes around values. |

More detail is available with `LOG_LEVEL=debug`.
