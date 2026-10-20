# telegram-ai-bot

A Telegram chat character written in Go: a playful, quick-witted group-chat
regular (called Maria unless you rename her) rather than a neutral assistant.
She answers through [Google Gemini](https://ai.google.dev), remembers
conversations in PostgreSQL, and can call a small set of tools that are
checked in code (current time, calculator, bot help, drawing, polls, a
group's admin list, moderation for group admins, and live data: exchange
rates, weather, prayer times, news headlines, Wikipedia, football and crypto
prices).

- In groups she answers when called by name, mentioned or replied to, as a
  reply to the message that addressed her, and follows the recent chatter.
- A reply that is only laughter, a sticker or a GIF gets an emoji reaction
  instead of words, and limits kept in the database keep her from flooding.
- A group shares one conversation with her; private chats stay private.
- Group admins can mute her with `/mute`, and have her delete, pin, mute or
  ban by replying to a message and then saying yes to her question; see
  [Groups](#groups).
- Replies in the chat's language (Uzbek, Russian, English).
- Long polling: no public port, domain or TLS certificate needed.
- One dependency besides the Go standard library: `pgx` for PostgreSQL.
- Tests run without Telegram or model provider credentials.

## Contents

1. [Architecture](#architecture)
2. [Configuration](#configuration)
3. [Local setup](#local-setup)
4. [Tests](#tests)
5. [Choosing a model](#choosing-a-model)
6. [The character](#the-character)
7. [Photos, stickers, GIFs and voice messages](#photos-stickers-gifs-and-voice-messages)
8. [Drawing pictures](#drawing-pictures)
9. [Live data](#live-data)
10. [Daily limit in private chats](#daily-limit-in-private-chats)
11. [Groups](#groups)
12. [VPS deployment (systemd)](#vps-deployment-systemd)
13. [Docker Compose deployment (optional)](#docker-compose-deployment-optional)
14. [Vercel deployment (webhook)](#vercel-deployment-webhook)
15. [Backup and restore](#backup-and-restore)
16. [Long polling and webhooks](#long-polling-and-webhooks)
17. [Security checklist](#security-checklist)
18. [Troubleshooting](#troubleshooting)

## Architecture

```text
cmd/bot/main.go                 long-polling command; migrate and webhook subcommands
api/webhook.go                  serverless function that receives updates by webhook
internal/app                    assembles the bot for either way of running it
internal/webhook                HTTP handler for webhook updates, with secret check
internal/config                 load .env + environment, validate
internal/telegram               Bot API client: long polling, sending, message splitting
internal/ai                     Model client: types, retries, tool-calling format
internal/tools                  tool registry + time, calculator, help, drawing, polls, admin list, moderation, owners' tools
internal/webapi                 live-data tools: rates, weather, prayer times, news, Wikipedia, football, crypto
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
3. The worker applies the per-user flood guard and, in a group, the ignore
   list, the emoji reaction to applause and the reply quota, then runs a
   command or calls `agent`, in a private chat only within the daily limit.
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
group_messages(id PK, chat_id, message_id, author_id, author_name, text,
               created_at)
groups(chat_id PK, title, username, bot_status, bot_rights jsonb,
       first_seen_at, last_seen_at)
image_generations(id PK, user_id, chat_id, created_at)
bot_replies(id PK, chat_id, bot_id, created_at)
group_replies(id PK, chat_id, user_id, created_at)
private_messages(id PK, user_id, denied, created_at)
broadcasts(id PK, owner_id, update_id, text, chat_ids bigint[],
           created_at, sent_at)
group_rules(id PK, chat_id (0 for every group), kind, user_id, user_name,
            text, set_by, created_at, expires_at)
moderation_actions(id PK, chat_id, action, requester_id, target_user_id,
                   target_message_id, target_text, minutes, reason,
                   update_id, created_at, UNIQUE (update_id, action))
pending_moderation(requester_id, chat_id, action, target_user_id,
                   target_message_id, target_name, target_text, minutes,
                   reason, update_id, prompt_message_id, created_at,
                   PK (requester_id, chat_id))
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

What is stored about people: the numeric Telegram ID and, in groups, each
sender's first name and `@username`. No phone numbers. Message text is
stored for messages addressed to the bot and for its answers, and, while the
group log is on, for every message seen in a group (see
[What she remembers](#what-she-remembers-and-what-she-does-not)); all of it
is deleted after `RETENTION_DAYS`. Owners' standing rules are kept until
they expire, and the moderation audit until you delete it; see
[Groups](#groups). The schema is
defined by the files in
[internal/storage/migrations/](internal/storage/migrations/), one file per
table holding the table as it is now (migrations 1 to 22 were merged into
them). A new database runs them and is recorded at version 22; a database
that ran the old migrations is already there and skips them. A database
part way through the old ones is refused: migrate it with a release from
before the merge first. To change the schema later, add a new file numbered
from 0023; do not edit one that has already been applied.

## Configuration

Settings come from environment variables. For local development, put them in
a `.env` file in the directory you start the bot from; variables already set
in the environment win over the file.

| Variable                      | Default                        | Meaning                                                                                                      |
| ----------------------------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `TELEGRAM_BOT_TOKEN`          | required                       | Token from [@BotFather](https://t.me/BotFather)                                                              |
| `GEMINI_API_KEY` | one of the two keys | Google Gemini key from <https://aistudio.google.com/apikey>; set this or `XAI_API_KEY`, not both |
| `GEMINI_MODEL` | `gemini-3.6-flash` | Gemini model ID, used with `GEMINI_API_KEY` |
| `FALLBACK_MODELS` | Gemini: `gemini-3.5-flash,gemini-flash-latest,gemini-3.5-flash-lite` | Models tried in order when the main one is overloaded, out of quota or does not answer, then once more after a pause; a model out of quota is skipped for 15 minutes, one that did not answer within 20 seconds for 10; `none` turns this off |
| `BACKUP_API_KEY` | empty | Key for a backup provider, asked when every model of the main one failed; replies that already moved to it stay on it |
| `BACKUP_BASE_URL` | `https://api.groq.com/openai/v1` | The backup's OpenAI-compatible API (Groq by default). Groq's free tier allows 8,000 tokens a minute, which is one group reply at most: a request with the persona and the group log is 6,000 tokens or more |
| `BACKUP_MODEL` | `openai/gpt-oss-120b` | The backup's model; it gets voice messages as transcripts only |
| `XAI_API_KEY` | one of the keys | xAI (Grok) key from <https://console.x.ai>; images yes, voice no |
| `XAI_MODEL` | `grok-4` | Grok model ID, used with `XAI_API_KEY` |
| `MAX_MEDIA_MB` | `5` | Largest photo or sticker file downloaded for the model (1 to 20) |
| `MAX_VOICE_SECONDS` | `120` | Longest voice message passed to the model |
| `ENABLE_IMAGE_GENERATION` | `true` | Let the bot draw pictures (Gemini only; needs billing on the Gemini key) |
| `IMAGE_MODEL` | `gemini-3.1-flash-image` | Gemini model that draws |
| `DAILY_IMAGE_LIMIT` | `2` | Pictures per user per day; owners have no limit; `0` lets only owners draw |
| `FOOTBALL_DATA_TOKEN` | empty | Free key from <https://www.football-data.org/client/register>; without it there is no football tool. See [Live data](#live-data) |
| `COINGECKO_DEMO_KEY` | empty | Optional CoinGecko demo key from <https://www.coingecko.com/en/api>; without it crypto prices share CoinGecko's keyless quota |
| `DB_POSTGRES_URL`             | required                       | PostgreSQL connection string                                                                                 |
| `TELEGRAM_WEBHOOK_SECRET` | empty | Only for webhook deployments; see [Vercel deployment](#vercel-deployment-webhook) |
| `CRON_SECRET` | empty | Only on Vercel: protects the daily cleanup endpoint |
| `MAX_OUTPUT_TOKENS`           | `1024`                         | Longest model reply, in tokens                                                                               |
| `BOT_DISPLAY_NAME`            | `Maria`                         | The character's name                                                                                         |
| `PERSONA_PROMPT`              | built-in                       | The character's personality and rules; `{name}` is replaced by the name. See [The character](#the-character) |
| `MAX_HISTORY_MESSAGES`        | `20`                           | Recent messages sent to the model as context                                                                 |
| `MAX_TOOL_CALLS_PER_TURN`     | `4`                            | Tool calls allowed while answering one message                                                               |
| `MAX_CONCURRENT_REQUESTS`     | `10`                           | Number of workers                                                                                            |
| `USER_RATE_LIMIT_PER_MINUTE`  | `10`                           | Messages each user may send per minute                                                                       |
| `PRIVATE_DAILY_LIMIT` | `10` | Answers each user gets per day in private chats, from midnight in `GROUP_HISTORY_TIMEZONE`; the next message gets one notice, later ones nothing. Owners have no limit (admins do); commands and failed turns are not counted; `0` turns it off. Counted in the database |
| `PRIVATE_CHATS` | `on` | Whom she answers in private chats: `on` everyone, `owners` the owners only, `off` nobody, so that she works in groups only. A message that is not answered gets nothing at all, not even a notice. With `off`, owners cannot use private-chat tools such as broadcasts either |
| `REQUEST_TIMEOUT_SECONDS`     | `45`                           | Time budget for answering one message                                                                        |
| `MAX_INPUT_CHARS`             | `4000`                         | Longest user message accepted                                                                                |
| `RETENTION_DAYS` | `1` | Chat history is kept this many days, then deleted; `0` keeps it forever |
| `ADMIN_USER_IDS`              | empty                          | Comma-separated Telegram user IDs of administrators                                                          |
| `ADMIN_COMMANDS`              | empty                          | Comma-separated commands only administrators may use, e.g. `model,about`                                     |
| `ENABLE_WAKE_WORD_TRIGGER`    | `true`                         | In groups, answer when called by name; needs privacy mode off, see [Groups](#groups)                         |
| `BOT_WAKE_WORDS`              | the display name               | Comma-separated names the bot answers to, each one word, e.g. `maria,mariya,мария,мариа` (not `mira`: another bot has that name) |
| `ENABLE_USERNAME_MENTION`     | `true`                         | In groups, answer when `@username` is mentioned                                                              |
| `ENABLE_REPLY_TRIGGER`        | `true`                         | In groups, answer replies to the bot's messages                                                              |
| `GROUP_AMBIENT_MESSAGES` | `500` | How many of the last 14 hours' group messages are given to the model with a question (0 to 500; 0 turns the group log off) |
| `GROUP_HISTORY_TIMEZONE` | `Asia/Tashkent` | The groups' time zone: the times in the group log and in what the model is told, the day the picture and private-chat limits count, and the time given when nobody names a zone |
| `BOT_OWNER_USERNAME` | empty | The owners' Telegram usernames, comma-separated (e.g. `pronium,TursunoffM`); the bot treats owners warmly, follows their instructions and lets them manage it in any group |
| `BOT_OWNER_IDS` | empty | The owners' numeric Telegram user IDs, comma-separated; when set, only these accounts are owners and `BOT_OWNER_USERNAME` (then required) only names them. Safer: a username can be given up and taken by someone else |
| `ALLOWED_BOTS` | empty | Other bots the bot answers in groups, comma-separated usernames, or `*` for any bot; empty answers no bots |
| `BOT_REPLIES_PER_10_MIN` | `5` | Most replies to bots per group in 10 minutes, so two bots cannot talk without end |
| `GROUP_CONTEXT_MODE`          | `shared`                       | Only `shared` is supported: one conversation per group                                                       |
| `MAX_GROUP_REPLY_LENGTH`      | `4000`                         | Longest answer posted in a group (at most 4096; an emoji usually counts as two)                              |
| `GROUP_RATE_LIMIT_PER_MINUTE` | `6`                            | Answers per group per minute, across all members; past it she is silent. Counted in the database             |
| `MEMBER_REPLIES_PER_10_MIN`   | `4`                            | Answers each member gets per group in 10 minutes; past it she is silent with them. Counted in the database    |
| `LOG_LEVEL`                   | `info`                         | `debug`, `info`, `warn` or `error`                                                                           |
| `GEMINI_BASE_URL`             | Google's OpenAI-compatible API | Override for testing or a proxy; `XAI_BASE_URL` likewise for Grok                                            |
| `TELEGRAM_API_URL`            | `https://api.telegram.org`     | Override for testing or a local Bot API server                                                               |

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
# Edit .env: set TELEGRAM_BOT_TOKEN, GEMINI_API_KEY and, for example,
# DB_POSTGRES_URL=postgres://localhost:5432/tgbot?sslmode=disable

# 3. Create the tables
make migrate

# 4. Start the bot
make run
```

Send `/start` to your bot in Telegram. Stop the bot with Ctrl+C; it finishes
the message it is working on before exiting.

Bot commands: `/start`, `/help`, `/reset` (delete the conversation),
`/model`, `/about`, and in groups `/mute`, `/unmute`, `/ignore` and
`/unignore`.

## Tests

```bash
go test ./...          # all tests
go test -race ./...    # with the race detector
go vet ./...
gofmt -w .
make check             # vet + race tests + formatting check
```

No test talks to Telegram or a model provider; both are replaced by local fake
servers.

The storage tests and the end-to-end test need a real PostgreSQL database and
are skipped unless `TEST_DB_POSTGRES_URL` is set:

```bash
createdb tgbot_test
TEST_DB_POSTGRES_URL='postgres://localhost:5432/tgbot_test?sslmode=disable' go test -p 1 ./...
```

**Use a disposable database.** These tests drop and recreate the bot's
tables. `-p 1` runs packages one after another so they do not share the
database at the same time.

## Choosing a model

The bot works with **Google Gemini** or **xAI Grok**. Which one is used
depends on which key is set: `GEMINI_API_KEY` or `XAI_API_KEY`. Set exactly
one; with both the bot refuses to start. OpenRouter is no longer supported:
with only `OPENROUTER_API_KEY` set, the bot refuses to start and says to set
`GEMINI_API_KEY`.

**Grok.** Create a key at <https://console.x.ai> and set `XAI_API_KEY`. The
model defaults to `grok-4`; set `XAI_MODEL` for another. Grok sees photos
and stickers but cannot listen to voice messages here; for those, use
Gemini.

**Gemini.** Create a key at <https://aistudio.google.com/apikey> and set
`GEMINI_API_KEY`. The model defaults to `gemini-3.6-flash`, a fast full flash model
that answered in about 1.5 s in testing; change it any time by setting
`GEMINI_MODEL` for another one from
<https://ai.google.dev/gemini-api/docs/models>. Gemini models support the
tools (time, calculator) and answer within seconds, which matters for the
60-second limit on Vercel. The bot talks to
Gemini through Google's OpenAI-compatible endpoint, so nothing else changes.
On Vercel, set `GEMINI_MODEL` (and, if you like, `FALLBACK_MODELS`) under
Project Settings → Environment Variables and redeploy: the variable always
wins over the default in the code.
The free tier has per-minute and per-day request limits; when they are hit
the user gets the generic error and the log shows `status 429`.

When Google answers that a model is overloaded (`503`), that the account is
over its limits (`429`), or does not answer within 20 seconds, the same
request is sent to the next model in `FALLBACK_MODELS`. Each new message
starts again with the main model, unless it ran out of quota (skipped for 15
minutes) or did not answer in time (skipped for 10 minutes, so a slow model
does not make every reply wait 20 seconds). If every model is busy, the user is told
the AI is overloaded and to try again in a minute.

When every Gemini model failed and `BACKUP_API_KEY` is set, the request
goes to the backup provider, Groq by default (see
[Configuration](#configuration) for its free-tier limit).

If the model rejects a request because it cannot use tools, the bot logs
`model does not support tool calling; continuing without tools`, answers
without tools, and stays in that mode until it is restarted. In that mode the
model may guess at the time or at arithmetic.

## The character

The bot plays a character instead of acting as an assistant. Out of the box
she is **Maria**: funny, playful and warm, calm when pushed, and good
company rather than the group's troll. She teases only someone who is
bantering with her, once, and answers a jab with one light line. She keeps
banter under about 15 words and most answers to a sentence or two, like a
real chat member, and answers in the language and script of the message in
front of her: Uzbek in Cyrillic gets Cyrillic. The code reads that language
from the message's letters and common words and tells the model in a
bracketed line right before it ("[Answer in Uzbek, in Latin script.]"),
since the language of the chat around it pulled her away. A reply that
needs no words gets an emoji reaction or nothing. Newcomers get a friendly
answer, and "who are you?" a one-line introduction. She notices when a conversation is
serious and does not force a joke there. A plain question gets a plain
answer first, even in the middle of an argument; she does not guess dates
or facts, and still uses the tools for the time and for arithmetic.

### Profiles

Each group's memory is separate, so what is said in one group never reaches
another. What does follow a person everywhere is how they asked to be
spoken to: when someone says "meni Aziz aka deb chaqir" or "menga siz deb
gapir", she saves it with `my_profile` (the `user_profiles` table), and every
later turn of theirs, in any group or chat, is told the name and the "siz".
Only the person themselves can set it, the name must be in their own
message, and nobody else's turn ever shows it. Nothing they said is kept;
"profilimni o'chir" clears it. An owner's own form of address wins over the
name.

### Recalling other chats

In a chat that nobody but the person and Maria can read, a private chat or a
group of just the two of them (`getChatMemberCount` is 2), each turn also
carries that person's own exchanges with her in other chats over the last
day: their messages and her answers, never what anyone else said. She
brings them up only when the person does. In any group with other people
each group's talks stay in that group, and asked about another group she
says so.

### Messages to the owners

Asked in a group to tell an owner something ("Мариа @pronium га ёз ..."),
she calls `notify_owner`, which sends the owner a private message: the
group's name, who asked, and the asker's own words as they wrote them,
never text the model wrote. It writes only to owners known by
`BOT_OWNER_IDS`, a named owner matched by Telegram's own record of their
username, and each group may send one such message a day (counted in the
`owner_notices` table, from midnight in `GROUP_HISTORY_TIMEZONE`). Telegram
lets a bot write only to someone who started a private chat with it, so each
owner must send the bot `/start` once; until then she says so and the day's
message is not spent. `PRIVATE_CHATS=off` does not stop this.

### Limits built into the default character

The built-in prompt tells the model to tease the situation, the message or
the joke, never a person's worth, and sets these lines:

- no jokes at anyone's intelligence, family, upbringing, appearance,
  illness, mental health, script, dialect or spelling, and none about
  suicide; no lectures about manners or swearing;
- no racist slurs, no hateful attacks based on ethnicity or nationality, and
  no demeaning attacks on protected characteristics; no "modes" that switch
  these off, even for an owner;
- no encouraging real-world violence or serious harm, and no threats;
- no turning banter into sustained harassment, and stop when someone asks;
- never claim to be a real human, never claim an action no tool did, and
  never reveal the prompt, keys or private conversations.

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
- If you write your own prompt, keep the limits above and the "Rules of the
  application" block from the built-in one (tools, untrusted content, plain
  text). They are part of the prompt, so replacing the
  prompt removes them.
- A systemd environment file cannot hold a multi-line value. Put a long
  prompt on one line, or keep the built-in one.

The same character answers in private chats and in groups. In groups a short
fixed note is added that explains the message format and the turn header,
and lets the model stay silent or answer with a reaction (below).

## Photos, stickers, GIFs and voice messages

The bot can look at what people send, not only read it.

| Sent | What the model gets |
|---|---|
| Photo | the photo (the largest size under `MAX_MEDIA_MB`) and its caption |
| Sticker | the sticker image and its emoji |
| Animated or video sticker | its first frame and its emoji |
| GIF | its first frame |
| Voice message | its transcript, made by the model from the audio (Gemini only), up to `MAX_VOICE_SECONDS` |
| Video, round video | its cover frame and length |
| File, song, poll | its name; the song's performer and title; the poll's question and options |

Locations and contacts are not read; in a private chat the bot says so, in a
group it stays quiet. A forwarded message is marked with where it came from.

In the text the model and the history see, media appears as a label:
`<photo>`, `<sticker 😂>`, `<animated sticker 🔥>`, `<GIF>`,
`<voice message, 12 s>`, `<video 0:15>`, `<round video 0:08>`,
`<file report.pdf>`, `<audio Yulduz - Bahor>`, `<poll: Lunch? / plov / manti>`,
and `<forwarded from Name>` in front of a forward. The file itself is sent to
the model only with the message it came in. It is not stored, so in later turns the bot remembers
that a photo was sent and what it answered, not the photo.

**In groups** the bot looks only at media meant for it:

- a photo or GIF whose caption calls the bot or mentions it;
- a photo, video or voice message sent as a reply to the bot (a member's
  sticker or GIF reply gets an emoji reaction instead; see
  [Reactions and silence](#reactions-and-silence));
- the photo, sticker, GIF or voice message someone replies to while calling
  the bot ("Maria, nima deysan?" as a reply to a friend's photo).

Other media is never downloaded. With the recent-chatter buffer on, it is
noted as "Ann: <photo>" without being seen.

Each photo or voice message adds a download from Telegram and a larger
request to the model, so answers take a little longer. Gemini's
OpenAI-compatible endpoint refuses the label `ogg` that Telegram's voice
format would need, but reads the audio from its bytes; voice messages are
therefore sent unchanged with the accepted label `mp3`.

Voice messages are turned into text first, and the transcript is what the
model reads and what is stored: `<voice message, 38 s> "..."`. In groups
every voice message is transcribed as it arrives, even when it does not
address the bot, so that "what did he say?" or "write it as text" can be
answered later from the group log. Each one costs one extra model request.

## The owner

Set `BOT_OWNER_USERNAME` to your Telegram username (with or without `@`);
for several owners, separate them with commas. The bot then knows you made
it: it is friendly to you, does what you ask in
the chat within its limits, and lets you use `/mute`, `/unmute`, `/reset`,
`/ignore` and `/unignore` in any group, and, once `BOT_OWNER_IDS` is set,
have it moderate wherever it holds the admin right (see
[Moderation](#moderation)). In groups every
sender is shown to the model by first name and `@username`.
Your messages are additionally marked `[owner]`, based on your
Telegram account, so someone who copies your name or says they are the owner
gets no special treatment. Owners have no daily limit, on answers in private
chats or on pictures.

Also set `BOT_OWNER_IDS` to the owners' numeric Telegram IDs (the bot logs
`user_id=` for each message). Then only those accounts are owners, and the
usernames merely name them in the prompt: a username can be given up and
later taken by someone else, an ID cannot. Without it, owners moderate only
where they are admins with the right, like anyone else, and are not spared
by moderation; the bot logs a warning at startup.

Owners can give the bot orders that last, with the owner-only `group_rules`
tool; see [Standing rules and the ignore list](#standing-rules-and-the-ignore-list).

Owners can also ask the bot about its own data ("Maria, nechta guruhdasan?"),
which it looks up with the owner-only `bot_database` tool:

- `overview`: how many groups it has been in, users, and today's messages and
  pictures.
- `groups`: every group it knows, with whether it is still there, whether it
  is an admin there and the member count, all checked live with Telegram.
- `messages`: today's messages of one group. Another group's messages are
  given only in a private chat with the owner, so they never land in a
  different group.

Owners can also have the bot post in its groups ("Maria, guruhlarga master
qaytdi deb yoz") with the owner-only `send_to_groups` tool. It works in two
steps: the bot first shows the exact text and the groups, and sends only after
the owner says yes in a later message, within 10 minutes. The code refuses a
confirmation made in the same message as the draft, so the model cannot send
on its own.

Groups are recorded when the bot is added, promoted or removed (Telegram's
`my_chat_member` updates) and from the messages it logs. Telegram has no way
to list a bot's groups, so a group the bot joined before this, where nobody
has written since, is not known. After upgrading, run `set-webhook` again so
Telegram starts sending `my_chat_member`. The
`groups` table keeps only each group's name and is not expired; messages
still are.

## Drawing pictures

With a Gemini key the bot can draw. When someone asks for a picture ("Maria,
mushuk chizib ber"), the model calls the `generate_image` tool with an
English prompt, the picture is drawn by `IMAGE_MODEL`, and it is posted as a
reply with the bot's answer as its caption.

- Owners (`BOT_OWNER_USERNAME`) may draw as much as they like.
- Everyone else gets `DAILY_IMAGE_LIMIT` pictures a day (default 2),
  counted per user in the `image_generations` table from midnight in
  `GROUP_HISTORY_TIMEZONE`. The count is safe with several copies of the bot
  running at once. A picture that fails to draw does not count.
- Over the limit, the model tells the person they can get more tomorrow.

**Gemini's free tier does not include image generation**: with a free key
every request is refused with `429 You exceeded your current quota`, and the
bot says drawing is not available right now. Turn on billing for the key in
Google AI Studio to use it. Each picture is billed by Google.

## Live data

Seven tools look things up on public web APIs, so she does not guess numbers.
Every deployment has them; football needs `FOOTBALL_DATA_TOKEN`. Each result
names its source, and the persona tells her to say it in a few words, to call
currency and crypto prices data rather than advice, and to say a tool did not
answer instead of making up a number.

| Tool | What | Source and licence |
| --- | --- | --- |
| `currency_rate` | Exchange rates in so'm, gold and silver per gram | Central Bank of Uzbekistan (cbu.uz), official rates; gold and silver are an approximate world price from currency-api (jsDelivr mirror as a fallback) |
| `get_weather` | Current weather, up to 3 days of forecast, air quality | Open-Meteo (CC BY 4.0), MET Norway as a fallback; places from GeoNames (CC BY 4.0). She says which place it matched |
| `prayer_times` | Prayer and Ramadan times | Aladhan; approximate, the Muslim Board of Uzbekistan (muslim.uz) is official |
| `news_headlines` | Latest headlines in Uzbek or Russian | RSS feeds of kun.uz, gazeta.uz, uza.uz and daryo.uz; headlines and links only |
| `wiki_lookup` | Article summaries, "on this day" | Wikipedia (CC BY-SA) |
| `football` | League tables, the current matchday's matches, top scorers | football-data.org free tier; scores can lag, so "latest", not live |
| `crypto_price` | Coin prices in dollars and sums | CoinGecko |

The tools share one HTTP client with short timeouts and a small in-memory
cache, send `MariaBot/1.0 (+https://t.me/<bot username>)` as the user agent,
and need no key except football. Text from outside sites comes back marked as
untrusted in the tool descriptions.

## Daily limit in private chats

In a private chat each user gets `PRIVATE_DAILY_LIMIT` answers a day
(default 10), counted from midnight in `GROUP_HISTORY_TIMEZONE`.

- Counted: every message that reaches the model, photos and voice messages
  included, and one the model chose not to answer. Not counted: commands, a
  message too long or of a kind she cannot read, a message Telegram delivers
  twice (it counts once), and a turn that failed with an error (the model
  overloaded, out of time, or any other error), unless she drew a picture
  before the error: the picture is still sent, and counted.
- The first message past the limit gets one notice, in Uzbek, Russian and
  English; later ones that day get nothing. A voice message past the limit is
  not even transcribed.
- Owners have no limit. Everyone else has, the administrators in
  `ADMIN_USER_IDS` included. Groups are not affected; they have their own
  [limits](#limits).
- The count is kept in the database (`private_messages`: the user's ID and
  the time of each answer, never the text), so it holds on Vercel and across
  restarts. When it cannot be read she answers anyway and logs a warning:
  the limit guards the model's cost, and one answer too many disturbs
  nobody else.
- `0` turns the limit off.

## Groups

Add the bot to a group or supergroup like any other member. It speaks only
when addressed, and posts each answer as a reply to the message that
addressed it. It never starts talking on its own. A reply to it that needs
no words gets an emoji reaction instead, and limits kept in the database
keep it from taking over the chat.

### Ways to address the bot

| How                          | Example                                                 | Setting                         |
| ---------------------------- | ------------------------------------------------------- | ------------------------------- |
| Call it by name              | `Maria, nima gap?` · `Hey Maria, roast me` · `..., Maria?` | `ENABLE_WAKE_WORD_TRIGGER` (on) |
| Mention its username         | `@MyMariaBot hello`                                      | `ENABLE_USERNAME_MENTION` (on)  |
| Reply to one of its messages | reply to its answer with `and you?`                     | `ENABLE_REPLY_TRIGGER` (on)     |
| A command with its username  | `/mute@MyMariaBot`                                       | always on                       |

**Calling by name.** A wake word counts when it is a whole word, in any case,
and is the first word, the second word, or the last word of the message:

- Triggers: "Maria, come here." · "Hey Maria!" · "Maria nima qilyapsan?" ·
  "MARIA: gapir-chi." · "Voy Maria, qarang bunga." · "nima gap, Maria?"
- Does not trigger: "Marianna, hi" · "Mariaga ayt" · "Maria-ga ayt" · "I think
  Maria is wrong" · "we asked Maria about it yesterday" · `@maria`,
  `maria_bot`, `#maria` and links such as `maria.uz`

The call is removed before the text reaches the model ("Hey Maria, roast me"
becomes "roast me"). "Hey Maria" on its own is passed on as it is, and she
answers the greeting.

A sentence that merely begins with the name ("Maria is my sister's name")
does trigger. For that case the model is allowed to answer `[silent]`, and
the bot then posts nothing.

Matching is by exact spelling apart from upper and lower case. "Мария" in
Cyrillic, or a look-alike letter such as the Turkish dotted "İ", does not
match "maria" unless you add it:

`BOT_WAKE_WORDS=maria,mariya,мария,мариа`. Each wake word must be a single
word.

**Commands** in a group need the bot's username (`/help@MyMariaBot`). A plain
`/reset` is ignored, because it may be meant for another bot.

### Example

```text
Ann:   Go is interpreted, everyone knows that
Bek:   no way 😀
Bek:   Hey Maria, who is right?
Maria:  (replying to Bek)  Ann, Go is compiled. Bek wins this one.
Ann:   (replying to Maria)  xaxaxa
       (Maria puts 🤣 on Ann's message and writes nothing)
Ann:   anyway, lunch?
       (nothing: not addressed to her)
```

### What she remembers, and what she does not

- **Conversation:** the messages that addressed her and her answers,
  as one conversation per group. Each is labelled with the sender's first
  name so she can tell members apart and return to earlier jokes. The last
  `MAX_HISTORY_MESSAGES` of these are sent to the model.
- **The group log:** every message seen in the group (the sender's first
  name and `@username`, the time, up to 300 characters, media as labels such
  as `<photo>`, whom a reply answers, joins and leaves), including the ones
  that did not address her, is stored in the `group_messages` table. When
  she answers, the latest `GROUP_AMBIENT_MESSAGES` (default 500) of the last
  14 hours are given to the model as context, marked as untrusted, with
  times in `GROUP_HISTORY_TIMEZONE`; of her own lines only the last 5,
  shortened. The log is deleted with the rest of the history after
  `RETENTION_DAYS`, by `/reset`, and when the group is muted. Nothing is
  logged while a group is muted. Set `GROUP_AMBIENT_MESSAGES=0` to turn the
  log off.
- **Standing rules:** owners' orders and the ignore list are kept in the
  `group_rules` table until they expire, whatever `RETENTION_DAYS` says; an
  owner's form of address until it is removed. `/mute` and `/reset` do not
  touch them.
- **Moderation:** what she did, and for whom, is kept in the
  `moderation_actions` table until you delete it, including up to 500
  characters of each message she deleted. An action waiting for
  confirmation is kept in `pending_moderation`, with the member's ID and
  name and up to 500 characters of the message, until it is confirmed,
  cancelled or replaced.
- **The quoted message:** when someone replies to a message while addressing
  her, that one message is included, shortened to 1,000 characters. It is
  saved as part of the question.
- **Never mixed:** a private chat's history is not visible in any group, and
  one group's history is not visible in another.

Because a group's conversation is shared, what one member says to her can
come back in her answer to another member of the same group.

### Muting and resetting

| Command       | Who          | Effect                                                                              |
| ------------- | ------------ | ----------------------------------------------------------------------------------- |
| `/mute@Bot`   | group admins | She stops reacting in this group, and stops keeping recent messages, until unmuted. |
| `/unmute@Bot` | group admins | She is back.                                                                        |
| `/reset@Bot`  | group admins | Deletes the group's conversation and its recent-message buffer.                     |
| `/ignore@Bot`, as a reply to a member's message | group admins | She does not answer that member, or react to them, for 24 hours. |
| `/unignore@Bot`, as a reply to a member's message | group admins | She answers them again. |

"Group admins" are the group's owner and administrators as Telegram reports
them, plus anyone in `ADMIN_USER_IDS` and the bot's owners. The mute setting
is stored in the database and survives restarts. `/mute` and `/reset` leave
the standing rules alone. Admins, owners and the bot itself cannot be
ignored. In a private chat `/reset` deletes that user's own conversation and
needs no special rights.

To keep the bot out of groups altogether, use `/setjoingroups` in @BotFather.

### Reactions and silence

- A member's reply to one of her messages that is only applause gets an
  emoji reaction instead of an answer: 🤣 for laughter, otherwise one of
  👍 🔥 😁 👌. Applause is a sticker or GIF, text made only of laughter
  (`xaxa`, `ахах`, `lol`, `)))`; a single `ha` or `xa` is Uzbek for yes and
  laughs only with 😂 or `))` beside it), emoji, punctuation and digits, or
  `ok`. A question (`?`, `Ha?`), a sum (`2+2`), a sad face or sticker
  (😢 😭 💔), and a number answering her own question are not applause.
  It takes no model call and nothing from the reply quota. Calling her by
  name or `@username` is never applause, and owners always get an answer.
- The model may itself answer with one emoji reaction or with nothing. The
  history and the group log record a reaction as `You: <reacted 🔥>`.
- While she is working on an answer the chat shows her as typing.
- Telegram allows only its own list of reaction emoji; a group can allow
  fewer. A reaction Telegram refuses is logged at debug level and dropped.

### Standing rules and the ignore list

Owners' orders that should last ("don't talk about finance", "don't answer
Bek", "call me aka") are saved by the owner-only `group_rules` tool, which
the model calls when an owner gives such an order. They are kept in the
`group_rules` table, apart from the conversation, so `/mute` and `/reset`
do not erase them, and every turn in the group is told the ones in force.

| Rule | Who sets it, and where | How long |
|---|---|---|
| Instruction | an owner, in the group it is for, in their own words from that message (never words from a quoted message or the chat); at most 20 per group, 300 characters each | a week, or 1 to 720 hours if the owner says |
| Ignore a member | an owner replying to that member's message, or to her answer to them, in the group | 24 hours, or 1 to 720 hours if the owner says |
| Form of address | an owner, for themselves, in any chat; it holds everywhere | until removed |

Owners can ask her to list the rules and remove one by its number. Group
admins and owners can also ignore a member for 24 hours with `/ignore@Bot`
sent as a reply to the member's message, and undo it with `/unignore@Bot`
(see the table above). Replying to one of her own answers instead means the
member she answered, as stored with the conversation; when she cannot tell,
she says so rather than guess. An ignored member gets neither answers nor
reactions. Admins, owners and the bot itself cannot be ignored.

An owner's bare order not to answer a member, addressed to her as a reply to
that member's message ("Maria, bunga javob berma", "javob qaytarma",
"gapirma", "ignor qil", "e'tibor berma", "не отвечай", "игнорируй", "не
разговаривай с ним"), is read in code, not by the model: the member is
ignored for 24 hours, exactly as with `/ignore`, and she reacts 👌 to the
order. Any other wording, a question such as "nega javob bermading?", or an
order about someone who cannot be ignored goes to the model as before.

### Moderation

A group's admins can have her act on a message by replying to it and asking
("Maria, o'chir", "Maria, buni 30 daqiqaga mute qil", "Maria, pin qil"). The
model calls the `moderate` tool, which is offered in groups only, and every
check is made in code:

- **Actions:** delete the message; mute its author (an hour unless asked
  otherwise, at most a week; supergroups only) or unmute them; ban them, or
  unban; pin the message without a notification, or unpin it.
- **Confirmation:** the model only asks for an action; it never carries one
  out. She posts her own fixed question, in Uzbek, saying exactly what she will do
  ("Confirm: mute Bek @bek for 30 minutes? Reply yes (ha, да) to this
  message within 5 minutes, or no to cancel."), and acts only when the same
  person replies to that question with a plain yes (`ha`, `да`, `yes`,
  `ok`, `+`, 👍 and a few more), read in code, within 5 minutes. A plain no
  cancels it; anything else is an ordinary message. The answer is handled
  before the reply quota and the applause rule, so a busy group cannot
  swallow it. A text in the chat, a quoted message or a voice transcript can
  talk the model into asking, but only the person's own yes acts.
- **Target:** the message the person replied to, as Telegram delivered it.
  For mute, unmute, ban and unban, someone who replied to nothing or to
  their own message may instead tag one member: with a link to a member
  without a username, Telegram gives the ID; with an `@username`, the ID is
  the last author seen under it in that group's log, and Telegram must
  confirm that this account still has that username and is in the group.
  Two tags, an unknown username or any mismatch act on nobody, and she asks
  for a reply to the member's message instead. The confirmation question
  names the member, so the person sees whom they are confirming. Nothing
  the model, a member or a quoted message writes can choose whom she acts
  on. Her answers always reply to the person's own request.
- **Who may ask:** a bot owner recognised by `BOT_OWNER_IDS` (not by
  username alone), or the group's creator or an administrator
  who holds the matching right, as Telegram reports it at that moment:
  *delete messages* for delete; *ban users* for mute, unmute, ban and
  unban; *pin messages* for pin and unpin.
- **Her rights:** she needs the same right herself, checked with Telegram
  each time. The model is told those of her rights in each group that a tool
  uses (delete, ban and mute, pin) so that it does not
  offer what she cannot do. Give her only the rights you want her to use.
- **Never a target:** the person asking (they may still have their own
  message deleted or pinned), the bot itself (its own messages may be
  deleted or pinned), the bot's owners recognised by `BOT_OWNER_IDS`, the
  group's admins, and Telegram's service accounts.
- **Limits:** at most 10 actions per person in 10 minutes. When Telegram
  cannot say who may do what, nothing is done.
- **Once only:** each action is recorded in `moderation_actions` under the
  Telegram update that confirmed it, before Telegram is called, so an update
  delivered twice is acted on once, and a request delivered twice asks once.
  A record is dropped only when Telegram refuses; when it does not answer,
  or a server's error comes back instead, it may have acted, so the record,
  and a deleted message's text, stay.

She is told to use the tool only when an admin or owner asks, never on her
own initiative, and never to say an action was done: the result is her own
fixed reply to the confirmation ("Done: ..." or "Not done: ...").

### Polls

Anyone in a group can ask her for a poll ("Maria, so'rovnoma qil: plov yoki
manti?"). She posts an anonymous poll in that group, with 2 to 10 options
and one or several answers allowed, at most one per group every 10 minutes.
Polls are counted in `moderation_actions` as `poll`. In a private chat she
cannot make one.

### Admins

Anyone in a group can ask her who its admins are or who the regular members
are. The `group_admins` tool asks Telegram for the
group's creator and administrators (`getChatAdministrators`, which needs no
admin right) and marks her own entry, her owners recognised by
`BOT_OWNER_IDS`, and other bots. No bot can read a group's full member list,
so everyone else who wrote in the messages she was given counts as a regular
member, and she says nothing about members who did not write. An admin who
stays anonymous in the group is listed only as an anonymous admin, never by
name. In a private chat the tool is not offered.

### Members

No bot can read a group's member list, so she keeps her own: the
`group_members` table notes everyone a group shows her, by ID, first name and
`@username` — a sender of any message, members who join or leave, and, in
groups where she is an admin, Telegram's `chat_member` reports of joins,
leaves, bans, mutes and promotions. No message text is kept there. Members
who left or were banned are forgotten 90 days after they were last seen,
by the daily cleanup.

Asked who is in the group, who the regular members are or how many there
are, she calls the `group_members` tool: the 50 members she saw most
recently, the admins among them marked from `getChatAdministrators`, her
owners marked, and the group's size from `getChatMemberCount`. The list is
not everyone: members who have not written or joined since she started
keeping it are missing, and she says so. An anonymous admin's messages are
sent as the group, so they never appear.

### Limits

- Group answers are cut to `MAX_GROUP_REPLY_LENGTH` so she posts one message.
  The length is counted the way Telegram counts it, in which an emoji usually
  counts as two.
- **Reply quota.** She answers at most `GROUP_RATE_LIMIT_PER_MINUTE` (6)
  messages per group per minute, and each member at most
  `MEMBER_REPLIES_PER_10_MIN` (4) times per group in 10 minutes. Past either
  limit she answers with a 👀 reaction only, without a notice, so that she is
  seen to have read the message. The counts are kept in the
  database (`group_replies`), so they hold on Vercel and across restarts. A
  turn that answers nothing (an update Telegram delivered again, or a failed
  turn) gives its answer back.
  Not counted: owners; other bots, which have their own limit (below);
  reactions to applause; answers to her moderation questions; and `/mute`,
  `/unmute`, `/ignore`, `/unignore` and `/reset` from those who may use
  them. Past the quota a group admin is still answered when their message
  replies to another member's, as asking to moderate or ignore them does.
  When the count cannot be read she stays silent and logs a warning.
- The per-user limit, `USER_RATE_LIMIT_PER_MINUTE`, is a flood guard kept in
  memory. In a private chat its first refusal is announced; in a group it
  never is.
- One group's messages are answered one at a time, in order.
- A message addressed to her in a group is stored and passed on as a single
  line: line breaks become spaces and square brackets become round ones. A
  private message keeps its lines, but its square brackets become round
  too, so nobody can type the bracketed note that marks the owner.
- The managing commands (`/mute`, `/unmute`, `/ignore`, `/unignore`,
  `/reset`) from those who may use them are not held back by the quotas, and
  a muted group gets no notices of any kind.
- Other bots: Telegram delivers another bot's messages only when
  *Bot-to-Bot Communication Mode* is enabled in @BotFather for at least one
  of the two bots. The bot answers bots listed in `ALLOWED_BOTS` (or any bot
  with `*`), in groups only, when they address it, and at most
  `BOT_REPLIES_PER_10_MIN` times per group in 10 minutes; after that it is
  silent. Bots cannot run commands. Messages sent on behalf of a chat are
  ignored: anonymous group administrators, members posting as a channel, and
  channel posts forwarded into a linked discussion group. An anonymous admin
  therefore cannot use `/mute`.
- The bot needs permission to send messages in the group, and for
  moderation the admin rights described under [Moderation](#moderation).
- When a group is upgraded to a supergroup, Telegram gives it a new chat ID.
  The conversation, the mute setting and the standing rules from before the
  upgrade no longer apply.
- In forum-style supergroups she answers in the topic of the message she
  replies to. This was not tested against a real forum.

### BotFather privacy mode

Telegram bots have _privacy mode_, which is **on** by default. With it on,
Telegram delivers to a bot in a group only messages that mention it by
`@username`, replies to its messages, commands addressed to it, and a few
service messages. "Maria, nima gap?" contains no mention, so the bot never
receives it.

**To be called by name and to follow the conversation, turn privacy mode
off:**

1. In [@BotFather](https://t.me/BotFather): `/setprivacy`, choose the bot,
   choose **Disable**.
2. Remove the bot from the group and add it again. Telegram applies the
   setting when the bot joins.

With privacy mode off, Telegram sends the bot every message in the group.
What the bot does with them is described under "What she remembers" above:
messages that do not address her go into the group log, which the model
sees for the last 14 hours and which is deleted after `RETENTION_DAYS`.
**Tell the group's members that the bot receives and stores their
messages.** Their text is sent to the model provider, as context, whenever
she answers.

**To run without that**, leave privacy mode on and set:

```dotenv
ENABLE_WAKE_WORD_TRIGGER=false
GROUP_AMBIENT_MESSAGES=0
```

She then answers `@username` mentions, replies and commands only, and never
receives the rest of the conversation. (A bot that is a group administrator
receives all messages whatever the setting.)

To check the setting: `/mybots` in @BotFather, choose the bot, then _Bot
Settings_ and _Group Privacy_.

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
GEMINI_API_KEY=your-gemini-key
DB_POSTGRES_URL=postgres://tgbot:RUNTIME_PASSWORD_HERE@127.0.0.1:5432/tgbot?sslmode=disable
LOG_LEVEL=info
```

Only root can edit this file, and only the `tgbot` user can read it.
`sslmode=disable` is fine here because the connection never leaves the
machine. A systemd environment file cannot hold a multi-line value; keep
`PERSONA_PROMPT` on one line or leave it out to use the built-in character.

### 6. Apply migrations

Migrations run as the owner role. The `migrate` command needs only
`DB_POSTGRES_URL`, not the bot's other secrets:

```bash
sudo -u tgbot env \
  DB_POSTGRES_URL='postgres://tgbot_owner:OWNER_PASSWORD_HERE@127.0.0.1:5432/tgbot?sslmode=disable' \
  /usr/local/bin/telegram-ai-agent migrate

# The running bot only needs to read the migration history.
sudo -u postgres psql -d tgbot -c \
  'REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM tgbot'
```

You should see `migrations finished applied=17` on a new database. Running it
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
- Decide about privacy mode; see [Groups](#groups).

## Docker Compose deployment (optional)

```bash
cp .env.example .env            # set TELEGRAM_BOT_TOKEN and GEMINI_API_KEY
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
docker compose up -d --build
docker compose logs -f bot
```

Compose starts PostgreSQL, applies migrations, then starts the bot.
`DB_POSTGRES_URL` is set by the compose file, so the value in `.env` is ignored.
The database publishes no ports. This setup uses a single database role for
simplicity; prefer the systemd deployment when you want the restricted
runtime role.

```bash
docker compose exec -T db pg_dump -U tgbot -Fc tgbot > tgbot.dump  # backup
docker compose pull && docker compose up -d --build                # update
```

## Vercel deployment (webhook)

On serverless hosting the bot cannot keep a polling connection open, so
Telegram is told to call a function instead. The function is
[api/webhook.go](api/webhook.go), served at `/api/webhook`; it handles one
update per request and sends the reply before it responds.

This path was tested locally against fake Telegram and model servers,
not on Vercel itself. In particular it is unverified that Vercel's Go runtime
accepts the Go version in `go.mod`; if the build fails on that, lower the
`go` line to a version Vercel supports.

### What is different from long polling

| | Long polling (`make run`, VPS) | Webhook (Vercel) |
|---|---|---|
| The last 14 hours of group messages as context | from the `group_messages` table | the same |
| Mute state | cached in memory | read from the database for each addressed group message |
| Group reply quotas, replies to bots, moderation and poll limits | in the database | the same |
| Per-user flood guard | exact | per instance only, so much weaker; do not rely on it |
| Order of messages in a group | one at a time, in order | concurrent requests are not ordered |
| Retention cleanup (`RETENTION_DAYS`) | hourly | daily, by Vercel Cron at 03:00 UTC |
| Time per message | `REQUEST_TIMEOUT_SECONDS` | must fit in the function's 60 seconds |

With BotFather privacy mode off, every group message invokes the function,
including the ones the bot ignores. Ignored messages return at once and
touch neither the database nor the model, but they count as invocations.

A model answer slower than about 60 seconds is cut off by the platform, and
Telegram then delivers the update again. Keep `REQUEST_TIMEOUT_SECONDS` at
45 or lower and prefer a fast model.

### Steps

1. **Database.** Apply migrations once from your machine, with
   `DB_POSTGRES_URL` pointing at the production database: `make migrate`.
2. **Settings.** In the Vercel project, under *Settings → Environment
   Variables*, set at least:

   | Variable | Value |
   |---|---|
   | `TELEGRAM_BOT_TOKEN` | the bot's token |
   | `GEMINI_API_KEY` | your key |
   | `DB_POSTGRES_URL` | the connection string (a Vercel database integration may set it for you) |
   | `TELEGRAM_WEBHOOK_SECRET` | a secret you generate: `openssl rand -hex 32` |
   | `CRON_SECRET` | another secret you generate: `openssl rand -hex 32` |

   Any other variable from [Configuration](#configuration) can be set the
   same way.
3. **Deploy.** Push to the branch Vercel builds. Then check the function:

   ```bash
   curl https://YOUR-APP.vercel.app/api/webhook     # prints: ok
   ```

   If it answers `webhook is not ready; see the function logs` (status 503),
   the function logs in Vercel name the missing or invalid setting.
4. **Point Telegram at it.** On your machine, with the same
   `TELEGRAM_BOT_TOKEN` and `TELEGRAM_WEBHOOK_SECRET` in `.env`:

   ```bash
   go run ./cmd/bot set-webhook https://YOUR-APP.vercel.app/api/webhook
   ```

5. Send the bot a message.

To go back to long polling: `go run ./cmd/bot delete-webhook`. A token works
with one mode at a time; while a webhook is set, `make run` fails with
`409 Conflict`.

### Deploying this change

The group manners and admin powers update (reactions, reply quotas in the
database, standing rules, moderation, polls, owners by ID, a daily limit in
private chats) needs, in this order:

1. **Apply migrations 0013 to 0019 before deploying**: `make migrate` with
   `DB_POSTGRES_URL` pointing at the production database, as in step 1
   above. They add tables and two columns, and 0018 replaces 0016's
   `pending_bans` with `pending_moderation`: a ban waiting for confirmation
   at that moment has to be asked for again. Nothing else existing is
   changed. The new version refuses to start on the old schema (the function answers
   503 `webhook is not ready; see the function logs`, and Telegram delivers
   the updates again later), while the old version keeps working on the new
   one.
2. **Set in Vercel**: `BOT_WAKE_WORDS=maria,mariya,мария,мариа`, and,
   optionally but preferably, `BOT_OWNER_IDS` with the owners' numeric IDs.
   The new limits default to `GROUP_RATE_LIMIT_PER_MINUTE=6`,
   `MEMBER_REPLIES_PER_10_MIN=4` and `PRIVATE_DAILY_LIMIT=10`; set them only
   to change that.
3. **Deploy.**
4. **Give Maria the admin rights she should use** in each group: *Delete
   messages*, *Ban users* (for mute and ban) and *Pin messages*. She does
   only what both she and the admin asking may do; leave out a right to keep
   her from using it.
5. **Clear each group's old conversation**: send `/reset@YourBot` in every
   group, as an admin or owner. Each turn replays the group's last
   `MAX_HISTORY_MESSAGES` (20) stored messages whatever their age, and the
   stored ones are the old put-downs, Latin answers to Cyrillic and "Xo'p
   bo'ladi, xo'jayin" lines that the rewritten character must not copy.
   `/reset` deletes the group's conversation and day log; standing rules
   stay. Waiting for `RETENTION_DAYS` to remove them works too, more slowly.

Run `set-webhook` again after this update: the bot now asks Telegram for
`chat_member` updates too, and Telegram keeps sending only the kinds named
when the webhook was last set. Until then the member list still fills from
messages, joins and leaves.

### Daily cleanup

`vercel.json` schedules a Vercel Cron job that calls `/api/cleanup` every
day at 03:00 UTC. It deletes chat history older than `RETENTION_DAYS`
(default 1 day) and conversations left empty; group mute settings are kept.
Vercel sends `CRON_SECRET` with the call, and the endpoint refuses any
request without it, so nobody else can trigger it. Without `CRON_SECRET`
set in the project the endpoint answers `503` and nothing is deleted.

Runs and their results are listed in the Vercel dashboard under the
project's *Cron Jobs* settings, where a run can also be started by hand.

### Security of the webhook

The URL is public, so anyone can send it requests. The function accepts an
update only when the `X-Telegram-Bot-Api-Secret-Token` header equals
`TELEGRAM_WEBHOOK_SECRET`; everything else gets `401`. Without that check
anyone could forge messages from any user ID, including an administrator's.
Treat the secret like the bot token, and change both together if either
leaks (set the new secret in Vercel, redeploy, run `set-webhook` again).

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

**Retention.** Messages older than `RETENTION_DAYS` (default 1 day) are deleted
by an hourly job, or daily by Vercel Cron. A user can delete their own history at any time with
`/reset`. The counts behind the limits (`image_generations`, `bot_replies`,
`group_replies`, `private_messages`) hold IDs and times, no message text, and
are deleted with the history. Standing rules are deleted when they expire. The moderation audit
(`moderation_actions`, `pending_moderation`) is never deleted automatically; delete
old rows yourself if you do not want to keep them. Backups keep deleted data
until they are rotated out.

## Long polling and webhooks

The bot uses long polling: it asks Telegram for new messages and waits up to
30 seconds for an answer. Limitations:

- **One instance per token.** Two processes polling with the same token
  receive `409 Conflict` and steal updates from each other. You cannot scale
  out by starting a second copy.
- **In-memory limits.** The per-user flood guard and the worker queues live
  in the process and reset on restart. The group quotas are in the database.
- **Missed messages expire.** Telegram keeps undelivered updates for 24
  hours. A longer outage loses older messages.
- **Polling and a webhook cannot be combined.** If a webhook is set for the
  token, polling fails with `409 Conflict` until it is removed.

Polling has one real advantage: the server needs no public address, domain or
certificate, and no inbound port.

A webhook receiver is included for serverless hosting; see
[Vercel deployment (webhook)](#vercel-deployment-webhook) for how to use it
and for what it gives up. It handles each update inside one request, so it
does not by itself make several instances share the per-user flood guard or
keep a group's messages in order.

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
- [x] Group messages are stored in `group_messages` and deleted after
      `RETENTION_DAYS`; the model is given only the last 14 hours of them.
      They are never written to the logs, and not stored at all while a
      group is muted.
- [x] Quoted messages and recent group messages are marked as untrusted and
      shortened. Square brackets are replaced in them, in author names and in
      group messages, and group messages and names are folded to one line,
      so that no text can imitate a marker or another speaker in the shared
      history.
- [x] `/mute`, `/unmute`, `/ignore`, `/unignore` and a group's `/reset` are
      limited to that group's administrators, checked with Telegram, the
      bot's own administrators and its owners.
- [x] Moderation is checked in code, never by the model: the target is the
      message the person replied to, from the Telegram update; the person
      asking must be an owner or an admin holding the right, and the bot
      must hold it too, both asked of Telegram at that moment; admins,
      owners and Telegram's service accounts are never targets, nor is the
      bot, except to delete or pin its own message; at most 10 actions per
      person in 10 minutes; nothing is done until the person who asked
      replies yes to the bot's own question, read in code; an update
      delivered twice acts once. When Telegram cannot be asked, nothing is
      done.
- [x] Only owners can give standing orders, and only owners and group
      admins can have a member ignored. Whom to ignore is the author of the
      message they replied to, taken from the update, or the member her own
      answer replied to, taken from the stored conversation; never from the
      model. Admins and owners cannot be ignored.
- [x] With `BOT_OWNER_IDS` set, owners are recognised by account ID, which
      cannot be passed on the way a username can. Without it, being an owner
      grants no moderation and no protection from it.
- [x] Input length, output tokens, tool output size, tool run time, tool
      calls per message and time per message are all limited.
- [x] Group reply quotas, the private chats' daily limit, replies to bots
      and polls are counted in the database, so they hold with several
      instances; per-user flood guard;
      fixed number of workers; bounded queues.
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
- [ ] Set a budget alert on the Google Cloud project behind the Gemini key.
- [ ] Give the bot only the group admin rights you want it to use, and set
      `BOT_OWNER_IDS`.
- [ ] Schedule backups, copy them off the server, and test a restore.
- [ ] Keep Ubuntu, PostgreSQL and Go dependencies updated
      (`go list -m -u all`, `govulncheck ./...`).
- [ ] If a token leaks, revoke it in @BotFather (`/revoke`) or on
      Google AI Studio and update the environment file.

Known limits:

- Conversation text is stored unencrypted in PostgreSQL. Use disk encryption
  if that matters for your users.
- Message text is sent to the model provider (Google, or Groq when the
  backup answers). Tell your users, and check the provider's data policy.
- In a group, anyone who can post can make the bot answer, within the rate
  limits. The answer is visible to the whole group, and it can draw on what
  other members said to the bot there.
- The character's limits on humour are prompt instructions, not a filter. A
  model can be talked past them, and an edgy character will sometimes offend.
  You are responsible for what your bot posts.
- First names and `@username`s of group members are stored with their
  messages. The moderation audit keeps up to 500 characters of each message
  the bot deleted, with who asked, until you delete it.
- A model can still be talked into saying untrue or unwanted things. The bot
  limits what it can _do_ (registered tools, with every permission checked
  in code), not what it can _say_. What it says about an action can still
  be wrong; the log and `moderation_actions` show what was done.

## Troubleshooting

| Symptom                                                   | Likely cause and fix                                                                                                                                                                                                                                                                          |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `invalid configuration: X is required`                    | The variable is missing. Check `.env` or `/etc/telegram-ai-agent/env`.                                                                                                                                                                                                                        |
| `database schema is out of date; run the migrate command` | Run `make migrate` locally, or step 6 on the server.                                                                                                                                                                                                                                          |
| `database is unreachable`                                 | PostgreSQL is not running, or host, port, user or password in `DB_POSTGRES_URL` is wrong. Try `psql "$DB_POSTGRES_URL"`.                                                                                                                                                                      |
| `permission denied for table ...`                         | The runtime role lacks grants. Run the `ALTER DEFAULT PRIVILEGES` statements from step 3 _before_ migrating, or grant on the existing tables: `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO tgbot; GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO tgbot;` |
| `telegram polling failed ... 409 Conflict`                | Another copy of the bot is running with the same token, or a webhook is set. Stop the other copy, or call `deleteWebhook`.                                                                                                                                                                    |
| The bot is silent in a group                              | It answers only when addressed. Check, in order: is the group muted (`/unmute@YourBot` by an admin)? Is privacy mode off, if you call it by name? Was the bot re-added after changing privacy mode? Is the sender an anonymous admin or another bot (ignored by design)? Is the sender on the ignore list, or has the reply quota run out (log line `group reply limit reached`)? |
| "Maria, ..." gets no answer but `@username` works          | Privacy mode is still on, or the bot was not re-added after it was turned off; see [Groups](#groups). Also check `ENABLE_WAKE_WORD_TRIGGER` and `BOT_WAKE_WORDS`.                                                                                                                             |
| The bot ignores `/reset` or `/mute` in a group            | Commands in groups need the bot's username: `/mute@YourBot`.                                                                                                                                                                                                                                  |
| "Only this group's admins can do that"                    | `/mute`, `/unmute`, `/ignore`, `/unignore` and `/reset` in a group are for its admins. Anonymous admins are not recognised; turn "Remain anonymous" off or add your ID to `ADMIN_USER_IDS`.                                                                                                                           |
| The bot answers messages that only mention the name       | A message that starts or ends with the name counts as a call. The model may stay silent in such cases but does not always. Choose a less common wake word.                                                                                                                                    |
| The bot is too rude, too tame, or out of character        | That is the model and the prompt. Try another model, or adjust `PERSONA_PROMPT`; see [The character](#the-character).                                                                                                                                                                         |
| `X is no longer supported; use Y` at startup              | A setting was renamed. Rename it in the environment file.                                                                                                                                                                                                                                     |
| The bot answers but not as a reply                        | The original message was deleted before the answer arrived; the answer is then sent as an ordinary message.                                                                                                                                                                                   |
| Group answers end in `…`                                  | They were cut to `MAX_GROUP_REPLY_LENGTH`.                                                                                                                                                                                                                                                    |
| She stops answering a busy group for a while              | The reply quota: the log shows `group reply limit reached`. Raise `GROUP_RATE_LIMIT_PER_MINUTE` or `MEMBER_REPLIES_PER_10_MIN`. `group reply count failed; staying quiet` means the database could not be asked.                                                                             |
| She puts an emoji on a reply instead of answering         | The reply was applause (a sticker, a GIF, laughter, emoji or a nod), or the model chose to react. Call her by name or `@username` to get words.                                                                                                                                |
| She says she cannot delete, mute, ban or pin              | She lacks that admin right in the group, the person asking lacks it, or the target is an admin or owner; see [Moderation](#moderation). Mute works only in supergroups.                                                                                                                         |
| `identify the bot: telegram: 401 Unauthorized` at startup | The bot token is wrong or was revoked.                                                                                                                                                                                                                                                        |
| "Something went wrong on my side"                         | Look at the log line `turn failed`. `status 401` or `400 API key not valid`: bad Gemini key. `status 429`: the free tier's limit is reached; wait or pick another model. `status 404`: the model ID does not exist.                                                                          |
| "That took too long to answer"                            | The model was slower than `REQUEST_TIMEOUT_SECONDS`. Raise it or use a faster model.                                                                                                                                                                                                          |
| Wrong time or wrong arithmetic                            | The model has no tool support and is guessing. Look for `model does not support tool calling` in the log and see [Choosing a model](#choosing-a-model). The time is given in `GROUP_HISTORY_TIMEZONE` unless another zone is asked for.                                                     |
| "You're sending messages too quickly"                     | Per-user rate limit, announced in private chats only. Raise `USER_RATE_LIMIT_PER_MINUTE` if it is too strict.                                                                                                                                                                                 |
| "That's today's limit", then silence, in a private chat   | The private daily limit: the log shows `private daily limit reached`. Raise `PRIVATE_DAILY_LIMIT`, or `0` to turn it off; owners are never limited. `private message count failed; answering anyway` means the database could not be asked. |
| "I'm handling a lot of messages right now"                | Worker queues are full. Raise `MAX_CONCURRENT_REQUESTS`, and `pool_max_conns` to match.                                                                                                                                                                                                       |
| The bot forgot the conversation                           | History is limited to `MAX_HISTORY_MESSAGES`, and idle conversations are deleted after `RETENTION_DAYS`.                                                                                                                                                                                      |
| Service will not start under systemd                      | `journalctl -u telegram-ai-agent -n 50`. Check the environment file's owner and mode, and that it has no quotes around values.                                                                                                                                                                |

More detail is available with `LOG_LEVEL=debug`.
