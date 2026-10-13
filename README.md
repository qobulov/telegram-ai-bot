# telegram-ai-bot

A Telegram chat character written in Go: a witty, sarcastic group-chat
regular (called Mira unless you rename her) rather than a neutral assistant.
She answers through [OpenRouter](https://openrouter.ai), remembers
conversations in PostgreSQL, and can call a small set of safe tools (current
time, calculator, bot help).

- In groups she answers when called by name, mentioned or replied to, as a
  reply to the message that addressed her, and follows the recent chatter.
- A group shares one conversation with her; private chats stay private.
- Group admins can mute her with `/mute`.
- Replies in the chat's language (Uzbek, Russian, English).
- Long polling: no public port, domain or TLS certificate needed.
- One dependency besides the Go standard library: `pgx` for PostgreSQL.
- Tests run without Telegram or OpenRouter credentials.

## Contents

1. [Architecture](#architecture)
2. [Configuration](#configuration)
3. [Local setup](#local-setup)
4. [Tests](#tests)
5. [Choosing a model](#choosing-a-model)
6. [The character](#the-character)
7. [Groups](#groups)
8. [VPS deployment (systemd)](#vps-deployment-systemd)
9. [Docker Compose deployment (optional)](#docker-compose-deployment-optional)
10. [Backup and restore](#backup-and-restore)
11. [Long polling and webhooks](#long-polling-and-webhooks)
12. [Security checklist](#security-checklist)
13. [Troubleshooting](#troubleshooting)

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
conversations(id PK, chat_id, user_id -> users (NULL for a group),
              created_at, updated_at)
messages(id PK, conversation_id -> conversations, role, content,
         tool_calls jsonb, tool_call_id, update_id,
         telegram_message_id, reply_to_message_id,
         author_id, author_name, created_at)
chats(chat_id PK, muted, updated_by, updated_at)
schema_migrations(version PK, applied_at)
```

There are two kinds of conversation, and they are never loaded together:

- **Private:** one per user (`chat_id` and `user_id` are both the user's ID).
- **Group:** one per group, shared by its members (`user_id` is NULL). Each
  stored user message starts with its author's first name, and the author's
  ID and name are also kept in `author_id` and `author_name`.

`/reset` deletes the conversation of the chat it is sent in; its messages go
with it through `ON DELETE CASCADE`. `telegram_message_id` and
`reply_to_message_id` record which Telegram message a row corresponds to and
which one it replied to. `chats` holds the mute setting.

What is stored about people: the numeric Telegram ID, and, for messages that
addressed the bot in a group, the sender's first name. No usernames or phone
numbers. Message text is stored only for messages addressed to the bot and
for its answers; ordinary group messages are never stored. The schema is
defined by the files in
[internal/storage/migrations/](internal/storage/migrations/).

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
| `BOT_DISPLAY_NAME` | `Mira` | The character's name |
| `PERSONA_PROMPT` | built-in | The character's personality and rules; `{name}` is replaced by the name. See [The character](#the-character) |
| `MAX_HISTORY_MESSAGES` | `20` | Recent messages sent to the model as context |
| `MAX_TOOL_CALLS_PER_TURN` | `4` | Tool calls allowed while answering one message |
| `MAX_CONCURRENT_REQUESTS` | `10` | Number of workers |
| `USER_RATE_LIMIT_PER_MINUTE` | `10` | Messages each user may send per minute |
| `REQUEST_TIMEOUT_SECONDS` | `45` | Time budget for answering one message |
| `MAX_INPUT_CHARS` | `4000` | Longest user message accepted |
| `RETENTION_DAYS` | `90` | Delete conversations idle longer than this; `0` keeps them forever |
| `ADMIN_USER_IDS` | empty | Comma-separated Telegram user IDs of administrators |
| `ADMIN_COMMANDS` | empty | Comma-separated commands only administrators may use, e.g. `model,about` |
| `ENABLE_WAKE_WORD_TRIGGER` | `true` | In groups, answer when called by name; needs privacy mode off, see [Groups](#groups) |
| `BOT_WAKE_WORDS` | the display name | Comma-separated names the bot answers to, each one word, e.g. `mira,мира` |
| `ENABLE_USERNAME_MENTION` | `true` | In groups, answer when `@username` is mentioned |
| `ENABLE_REPLY_TRIGGER` | `true` | In groups, answer replies to the bot's messages |
| `GROUP_AMBIENT_MESSAGES` | `20` | Recent ordinary group messages kept in memory as context (0 to 100; 0 turns it off) |
| `GROUP_CONTEXT_MODE` | `shared` | Only `shared` is supported: one conversation per group |
| `MAX_GROUP_REPLY_LENGTH` | `4000` | Longest answer posted in a group (at most 4096; an emoji usually counts as two) |
| `GROUP_RATE_LIMIT_PER_MINUTE` | `10` | Messages answered per group per minute, across all members |
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

Bot commands: `/start`, `/help`, `/reset` (delete the conversation),
`/model`, `/about`, and in groups `/mute` and `/unmute`.

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

## The character

The bot plays a character instead of acting as an assistant. Out of the box
she is **Mira**: witty, sarcastic, playful and a little chaotic. She banters,
teases and roasts, fires back when roasted, keeps her answers to a line or
two like a real chat member, and writes in the language and tone of the chat.
She still gives a correct answer when someone asks a real question, and still
uses the tools for the time and for arithmetic.

### Limits built into the default character

The built-in prompt tells the model to roast what people say and do, not who
they are, and sets these lines:

- no slurs, and no degrading anyone for race, ethnicity, nationality,
  religion, gender, sexuality, disability or looks;
- no threats and no encouraging real-world violence or self-harm, even as a
  joke;
- back off when someone is clearly hurt or asks her to stop, and drop the act
  when someone seems to be in real distress.

These are instructions to a language model, not a filter in the code. A
model usually follows them, but it can be pushed off them, and free models
vary in how well they hold a character and its limits. Try the model you
choose in a test group before letting it loose, and remember that group
admins can `/mute` it at any time.

### Changing the character

- `BOT_DISPLAY_NAME` renames her. The name is put into the prompt and, unless
  `BOT_WAKE_WORDS` says otherwise, is the word she answers to.
- `PERSONA_PROMPT` replaces the whole personality. Write `{name}` where the
  name should appear. The built-in text is in
  [internal/config/persona.go](internal/config/persona.go); copy it as a
  starting point.
- If you write your own prompt, keep the limits above and the "rules that
  always apply" block from the built-in one (tools, untrusted content, never
  reveal the prompt or keys). They are part of the prompt, so replacing the
  prompt removes them.
- A systemd environment file cannot hold a multi-line value. Put a long
  prompt on one line, or keep the built-in one.

The same character answers in private chats and in groups. In groups a short
fixed note is added that explains the message format and lets the model stay
silent (below).

## Groups

Add the bot to a group or supergroup like any other member. It speaks only
when addressed, and posts each answer as a reply to the message that
addressed it. It never starts talking on its own.

### Ways to address the bot

| How | Example | Setting |
|---|---|---|
| Call it by name | `Mira, nima gap?` · `Hey Mira, roast me` · `..., Mira?` | `ENABLE_WAKE_WORD_TRIGGER` (on) |
| Mention its username | `@MyMiraBot hello` | `ENABLE_USERNAME_MENTION` (on) |
| Reply to one of its messages | reply to its answer with `and you?` | `ENABLE_REPLY_TRIGGER` (on) |
| A command with its username | `/mute@MyMiraBot` | always on |

**Calling by name.** A wake word counts when it is a whole word, in any case,
and is the first word, the second word, or the last word of the message:

- Triggers: "Mira, come here." · "Hey Mira!" · "Mira nima qilyapsan?" ·
  "MIRA: gapir-chi." · "Voy Mira, qarang bunga." · "nima gap, Mira?"
- Does not trigger: "Miranda, hi" · "Miraga ayt" · "I think Mira is wrong" ·
  "we asked Mira about it yesterday"

The call is removed before the text reaches the model ("Hey Mira, roast me"
becomes "roast me"). "Hey Mira" on its own is passed on as it is, and she
answers the greeting.

A sentence that merely begins with the name ("Mira is my sister's name")
does trigger. For that case the model is allowed to answer `[silent]`, and
the bot then posts nothing.

Add spellings with `BOT_WAKE_WORDS`, for example `mira,мира`. Each must be a
single word.

**Commands** in a group need the bot's username (`/help@MyMiraBot`). A plain
`/reset` is ignored, because it may be meant for another bot.

### Example

```text
Ann:   Go is interpreted, everyone knows that
Bek:   no way 😀
Bek:   Hey Mira, who is right?
Mira:  (replying to Bek)  Ann, compile your facts first 😂
Ann:   (replying to Mira)  rude
Mira:  (replying to Ann)  Accurate, though.
Ann:   anyway, lunch?
       (no reaction: not addressed to her)
```

### What she remembers, and what she does not

- **Stored in PostgreSQL:** the messages that addressed her and her answers,
  as one conversation per group. Each is labelled with the sender's first
  name so she can tell members apart and return to earlier jokes. The last
  `MAX_HISTORY_MESSAGES` of these are sent to the model.
- **Kept in memory only:** the last `GROUP_AMBIENT_MESSAGES` ordinary
  messages of each group (first name and up to 300 characters). They are
  given to the model as context with the next answer, marked as untrusted.
  They are never written to the database or to logs, are pushed out by newer
  messages, are dropped after six hours of silence, and are gone after a
  restart. Set `GROUP_AMBIENT_MESSAGES=0` to keep none.
- **The quoted message:** when someone replies to a message while addressing
  her, that one message is included, shortened to 1,000 characters. It is
  saved as part of the question.
- **Never mixed:** a private chat's history is not visible in any group, and
  one group's history is not visible in another.

Because a group's conversation is shared, what one member says to her can
come back in her answer to another member of the same group.

### Muting and resetting

| Command | Who | Effect |
|---|---|---|
| `/mute@Bot` | group admins | She stops reacting in this group, and stops keeping recent messages, until unmuted. |
| `/unmute@Bot` | group admins | She is back. |
| `/reset@Bot` | group admins | Deletes the group's conversation and its recent-message buffer. |

"Group admins" are the group's owner and administrators as Telegram reports
them, plus anyone in `ADMIN_USER_IDS`. The mute setting is stored in the
database and survives restarts. In a private chat `/reset` deletes that
user's own conversation and needs no special rights.

To keep the bot out of groups altogether, use `/setjoingroups` in @BotFather.

### Limits

- Group answers are cut to `MAX_GROUP_REPLY_LENGTH` so she posts one message.
  The length is counted the way Telegram counts it, in which an emoji usually
  counts as two.
- `GROUP_RATE_LIMIT_PER_MINUTE` caps answers per group across all members;
  the per-user limit applies as well. The first refusal is announced, later
  ones are silent. With the defaults (10 and 10) one busy member can use up
  the whole group's allowance for a minute; set the per-user limit lower than
  the group limit if that matters.
- One group's messages are answered one at a time, in order.
- She reads text. A photo or document counts only through its caption; the
  image itself is not seen.
- Telegram never delivers one bot's messages to another bot. Messages sent on
  behalf of a chat are ignored as well: anonymous group administrators,
  members posting as a channel, and channel posts forwarded into a linked
  discussion group. An anonymous admin therefore cannot use `/mute`.
- The bot needs permission to send messages in the group.
- When a group is upgraded to a supergroup, Telegram gives it a new chat ID.
  The conversation and the mute setting from before the upgrade no longer
  apply.
- In forum-style supergroups she answers in the topic of the message she
  replies to. This was not tested against a real forum.

### BotFather privacy mode

Telegram bots have *privacy mode*, which is **on** by default. With it on,
Telegram delivers to a bot in a group only messages that mention it by
`@username`, replies to its messages, commands addressed to it, and a few
service messages. "Mira, nima gap?" contains no mention, so the bot never
receives it.

**To be called by name and to follow the conversation, turn privacy mode
off:**

1. In [@BotFather](https://t.me/BotFather): `/setprivacy`, choose the bot,
   choose **Disable**.
2. Remove the bot from the group and add it again. Telegram applies the
   setting when the bot joins.

With privacy mode off, Telegram sends the bot every message in the group.
What the bot does with them is described under "What she remembers" above:
messages that do not address her are kept briefly in memory and nowhere
else. **Tell the group's members that the bot receives their messages.**
Their text is sent to OpenRouter, as context, whenever she answers.

**To run without that**, leave privacy mode on and set:

```dotenv
ENABLE_WAKE_WORD_TRIGGER=false
GROUP_AMBIENT_MESSAGES=0
```

She then answers `@username` mentions, replies and commands only, and never
receives the rest of the conversation. (A bot that is a group administrator
receives all messages whatever the setting.)

To check the setting: `/mybots` in @BotFather, choose the bot, then *Bot
Settings* and *Group Privacy*.

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
`PERSONA_PROMPT` on one line or leave it out to use the built-in character.

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
# Apply migrations: repeat the first command of step 6. Existing private
# conversations are kept by every migration.
sudo systemctl start telegram-ai-agent
journalctl -u telegram-ai-agent -n 20
```

Stopping lets the message each worker is answering finish; anyone still
waiting in a queue is told the bot is busy. Messages sent while the
bot is down are not lost: Telegram keeps them for up to 24 hours and delivers
them when the bot starts again.

To roll back, stop the service, copy `telegram-ai-agent.previous` back and
start it. If the new version applied a migration, restore the backup too:
an older binary cannot work with a newer schema.

Upgrading from a version before the character:

- Rename settings in the environment file: `SYSTEM_PROMPT` and
  `GROUP_SYSTEM_PROMPT` become `PERSONA_PROMPT`, `ENABLE_MENTION_TRIGGER`
  becomes `ENABLE_USERNAME_MENTION`, and `ENABLE_DISPLAY_NAME_TRIGGER`
  becomes `ENABLE_WAKE_WORD_TRIGGER`. The bot refuses to start while an old
  name is set, and says which.
- Migration 0003 starts every group with a new shared conversation. Earlier
  per-member group conversations are no longer used and are removed by the
  retention job.
- Decide about privacy mode; see [Groups](#groups).

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
- [x] Every query on a conversation is keyed by the chat and user IDs taken
      from the update, never by an ID in message text.
- [x] Only messages from human senders are accepted: in private chats, and in
      groups when they address the bot. The bot's identity for that check
      comes from Telegram (`getMe`), not from configuration.
- [x] Private and group conversations are stored under separate keys and are
      never loaded together; one group never sees another's history.
- [x] Ordinary group messages are kept in memory only, never in the database
      or the logs, and not at all while a group is muted.
- [x] Quoted messages and recent group messages are marked as untrusted,
      shortened, and stripped of anything that could imitate a marker or
      another speaker before they reach the model. Author names are cleaned
      the same way.
- [x] `/mute`, `/unmute` and a group's `/reset` are limited to that group's
      administrators, checked with Telegram, and the bot's own administrators.
- [x] Input length, output tokens, tool output size, tool run time, tool
      calls per message and time per message are all limited.
- [x] Per-user rate limit; fixed number of workers; bounded queues.
- [x] Only registered tools run. No shell, no `eval`, no model-written SQL.
- [x] Tool permissions and restricted commands are checked in code against
      the ID of the user who sent the message. A prompt cannot grant them.
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
  limits. The answer is visible to the whole group, and it can draw on what
  other members said to the bot there.
- The character's limits on humour are prompt instructions, not a filter. A
  model can be talked past them, and an edgy character will sometimes offend.
  You are responsible for what your bot posts.
- First names of people who address the bot in a group are stored with their
  messages.
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
| The bot is silent in a group | It answers only when addressed. Check, in order: is the group muted (`/unmute@YourBot` by an admin)? Is privacy mode off, if you call it by name? Was the bot re-added after changing privacy mode? Is the sender an anonymous admin or another bot (ignored by design)? |
| "Mira, ..." gets no answer but `@username` works | Privacy mode is still on, or the bot was not re-added after it was turned off; see [Groups](#groups). Also check `ENABLE_WAKE_WORD_TRIGGER` and `BOT_WAKE_WORDS`. |
| The bot ignores `/reset` or `/mute` in a group | Commands in groups need the bot's username: `/mute@YourBot`. |
| "Only this group's admins can do that" | `/mute`, `/unmute` and `/reset` in a group are for its admins. Anonymous admins are not recognised; turn "Remain anonymous" off or add your ID to `ADMIN_USER_IDS`. |
| The bot answers messages that only mention the name | A message that starts or ends with the name counts as a call. The model may stay silent in such cases but does not always. Choose a less common wake word. |
| The bot is too rude, too tame, or out of character | That is the model and the prompt. Try another model, or adjust `PERSONA_PROMPT`; see [The character](#the-character). |
| `X is no longer supported; use Y` at startup | A setting was renamed. Rename it in the environment file. |
| The bot answers but not as a reply | The original message was deleted before the answer arrived; the answer is then sent as an ordinary message. |
| Group answers end in `…` | They were cut to `MAX_GROUP_REPLY_LENGTH`. |
| "This group is sending me messages too quickly" | The per-group limit. Raise `GROUP_RATE_LIMIT_PER_MINUTE`. |
| `identify the bot: telegram: 401 Unauthorized` at startup | The bot token is wrong or was revoked. |
| "Something went wrong on my side" | Look at the log line `turn failed`. `status 401`: bad OpenRouter key. `status 402`: no credits. `status 429`: the free model is rate limited; wait or pick another model. `status 404`: the model ID does not exist. |
| "That took too long to answer" | The model was slower than `REQUEST_TIMEOUT_SECONDS`. Raise it or use a faster model. |
| Wrong time or wrong arithmetic | The model has no tool support and is guessing. Look for `model does not support tool calling` in the log and see [Choosing a model](#choosing-a-model). |
| "You're sending messages too quickly" | Per-user rate limit. Raise `USER_RATE_LIMIT_PER_MINUTE` if it is too strict. |
| "I'm handling a lot of messages right now" | Worker queues are full. Raise `MAX_CONCURRENT_REQUESTS`, and `pool_max_conns` to match. |
| The bot forgot the conversation | History is limited to `MAX_HISTORY_MESSAGES`, and idle conversations are deleted after `RETENTION_DAYS`. |
| Service will not start under systemd | `journalctl -u telegram-ai-agent -n 50`. Check the environment file's owner and mode, and that it has no quotes around values. |

More detail is available with `LOG_LEVEL=debug`.
