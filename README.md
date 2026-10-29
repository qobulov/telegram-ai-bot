# telegram-ai-bot

A Telegram chat character written in Go: a playful, quick-witted group-chat
regular (called Maria unless you rename her) rather than a neutral assistant.
She answers through [Google Gemini](https://ai.google.dev), remembers
conversations in PostgreSQL, and can call a small set of tools that are
checked in code (current time, bot help, drawing, polls, dice, a
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
- Replies in the chat's language (Uzbek, Russian, English), and in any chat
  through inline mode (`@bot question?`).
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
11. [Inline mode](#inline-mode)
12. [Telegram Business](#telegram-business)
13. [Groups](#groups)
14. [VPS deployment (systemd)](#vps-deployment-systemd)
15. [Docker Compose deployment (optional)](#docker-compose-deployment-optional)
16. [Vercel deployment (webhook)](#vercel-deployment-webhook)
17. [Backup and restore](#backup-and-restore)
18. [Long polling and webhooks](#long-polling-and-webhooks)
19. [Security checklist](#security-checklist)
20. [Troubleshooting](#troubleshooting)

## Architecture

```text
cmd/bot/main.go                 long-polling command; migrate and webhook subcommands
api/webhook.go                  serverless function that receives updates by webhook
api/stats.go, api/game.go       serverless functions behind the dashboard and the game
public/stats, public/game       static pages: the group dashboard Mini App and the Tarozi game
internal/app                    assembles the bot for either way of running it
internal/miniapp                the dashboard's API: Telegram init data check, membership check, statistics
internal/game                   the Tarozi game: signed sessions, dealing rounds, scoring plays
internal/webhook                HTTP handler for webhook updates, with secret check
internal/config                 load .env + environment, validate
internal/telegram               Bot API client: long polling, sending, message splitting
internal/ai                     Model client: types, retries, tool-calling format
internal/tools                  tool registry + time, help, drawing, polls, dice, admin list, moderation, owners' tools
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
  writes. Nothing is ever passed to a shell or an interpreter.
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
star_payments(charge_id PK, user_id, first_name, username, stars, days,
              paid_at, refunded_at)
premium_users(user_id PK, until)
broadcasts(id PK, owner_id, update_id, text, chat_ids bigint[],
           copy_chat_id, copy_message_id, created_at, sent_at)
channels(chat_id PK, title, username, bot_status, bot_rights jsonb,
         first_seen_at, last_seen_at)
group_rules(id PK, chat_id (0 for every group), kind, user_id, user_name,
            text, set_by, created_at, expires_at)
moderation_actions(id PK, chat_id, action, requester_id, target_user_id,
                   target_message_id, target_text, minutes, reason,
                   update_id, created_at, UNIQUE (update_id, action))
join_requests(chat_id, user_id, user_chat_id, expires_at, created_at,
              PK (chat_id, user_id))
own_sticker_sets(name PK, owner_id, created_at)
sticker_uploads(id PK, user_id, chat_id, created_at)
picture_verdicts(file_unique_id PK, unsafe, reason, judge, created_at)
business_connections(id PK, user_id, user_chat_id, first_name, username,
                     can_reply, can_manage_stories, enabled, refused,
                     auto_reply, connected_at, updated_at)
business_chats(connection_id -> business_connections, chat_id, first_name,
               username, last_message_at, owner_wrote_at,
               PK (connection_id, chat_id))
business_replies(id PK, connection_id, chat_id, created_at)
business_checklists(connection_id -> business_connections, chat_id,
                    message_id, title, tasks jsonb, sent_at,
                    PK (connection_id, chat_id, message_id))
group_boosts(chat_id, boost_id, created_at, PK (chat_id, boost_id))
schema_migrations(version PK, applied_at)
```

There are two kinds of conversation, and they are never loaded together:

- **Private:** one per user (`chat_id` and `user_id` are both the user's ID).
- **Group:** one per group, shared by its members (`user_id` is NULL). Each
  stored user message starts with its author's first name, and the author's
  ID and name are also kept in `author_id` and `author_name`.
- **Business:** one per customer of an owner's
  [business account](#telegram-business): `chat_id` is the customer's chat
  with the account, which is the customer's ID, and `user_id` the owner's.
  It is never the customer's own private chat with the bot, whose `user_id`
  is the customer's.

A conversation's messages are deleted with it through `ON DELETE CASCADE`.
`telegram_message_id` and
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
them). A new database runs them and is recorded at version 22, then runs
any newer file; a database that ran the old migrations is already there and
skips them. A database
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
| `XAI_API_KEY` | one of the keys | xAI (Grok) key from <https://console.x.ai>; images yes, voice no |
| `XAI_MODEL` | `grok-4` | Grok model ID, used with `XAI_API_KEY` |
| `MAX_MEDIA_MB` | `5` | Largest photo or sticker file downloaded for the model (1 to 20) |
| `MAX_VOICE_SECONDS` | `120` | Longest voice message passed to the model |
| `ENABLE_IMAGE_GENERATION` | `true` | Let the bot draw pictures (Gemini only; needs billing on the Gemini key) |
| `IMAGE_MODEL` | `gemini-3.1-flash-image` | Gemini model that draws |
| `DAILY_IMAGE_LIMIT` | `2` | Pictures per user per day; owners have no limit; `0` lets only owners draw |
| `GIPHY_API_KEY` | empty | Key from <https://developers.giphy.com/dashboard/> (API, not SDK); lets her send a GIF now and then instead of a sticker. See [Stickers](#stickers) |
| `YOUTUBE_API_KEY` | empty | YouTube Data API v3 key from Google Cloud Console, not bound to a service account; without it there is no video search |
| `TMDB_TOKEN` | empty | TMDB API Read Access Token from <https://www.themoviedb.org/settings/api>; without it there is no movies tool |
| `GOOGLE_SAFE_BROWSING_KEY` | empty | Google Cloud API key restricted to the Safe Browsing API; lets owners turn on the link guard (`group_rules` `guard` `links`), which deletes links Google takes for malware, phishing or unwanted software. Google offers this API for non-commercial use |
| `CALENDARIFIC_API_KEY` | empty | Calendarific key from <https://calendarific.com> (free plan, monthly limit); adds the `holidays` tool. It is sent in a header, never in a URL |
| `IMGFLIP_API_KEY` | empty | Imgflip API key from <https://imgflip.com/api> (free, a Bearer key); adds the `make_meme` tool |
| `URLHAUS_AUTH_KEY` | empty | Free Auth-Key from <https://auth.abuse.ch/>; also lets owners turn on the link guard, and with both keys a link is deleted when either service lists it (URLhaus by its host) |
| `SIGHTENGINE_API_USER`, `SIGHTENGINE_API_SECRET` | empty | From <https://dashboard.sightengine.com/api-credentials>; let Sightengine judge the picture guard's pictures (`guard` `images`, which deletes nude or very suggestive pictures from anyone, admins and owners included: photos, stickers, pictures sent as files and the first frame of GIFs and videos), one operation of its monthly quota each (nudity only: with gore too, the guarded groups' two hundred pictures a day spent the free plan's month in days). Without them, or when Sightengine fails (its free plan answers `400: 30-day usage limit reached` once the month is spent), the bot's own model judges instead; see [Picture guard](#picture-guard). The guard needs neither |
| `CAS_API_KEY` | empty | Combot API key, from <https://combot.org> (sign in with Telegram, *Settings*, *API key*; *Reset API key* makes one), sent as a Bearer header. CAS now answers nobody without one: every question was `401 API key required`. With it the spam guard (`guard` `spam`) asks CAS and lols.bot; without it lols.bot alone, which needs no key. lols.bot is asked from its own records (`quick`, a third faster), and once more when it drops the connection, as it now and then reset one; a list that cannot answer is logged and the member left alone |
| `TAVILY_API_KEY` | empty | Free key from <https://app.tavily.com>; without it there is no web search. See [Live data](#live-data) |
| `FOOTBALL_DATA_TOKEN` | empty | Free key from <https://www.football-data.org/client/register>; without it there is no football tool. See [Live data](#live-data) |
| `COINGECKO_DEMO_KEY` | empty | Optional CoinGecko demo key from <https://www.coingecko.com/en/api>; without it crypto prices share CoinGecko's keyless quota |
| `DB_POSTGRES_URL`             | required                       | PostgreSQL connection string                                                                                 |
| `TELEGRAM_WEBHOOK_SECRET` | empty | Only for webhook deployments; see [Vercel deployment](#vercel-deployment-webhook) |
| `CRON_SECRET` | empty | Only on Vercel: protects the daily cleanup endpoint |
| `WEBAPP_URL` | empty | The Vercel deployment's `https://` address, such as `https://maria.vercel.app`, where `public/` is served: turns on the [dashboard](#dashboard-mini-app) (`/stats`, the private chats' menu button) and [the game](#the-game-tarozi) (`/game`, the `send_game` tool). Empty turns both off |
| `MAX_OUTPUT_TOKENS`           | `8192`                         | Longest model reply, in tokens, thinking included |
| `AI_REASONING_EFFORT`         | `low` (Gemini)                 | How hard Gemini thinks before answering: `none`, `minimal`, `low`, `medium`, `high`, or `default` for the model's own. Thinking is paid as output and takes from `MAX_OUTPUT_TOKENS`; the lite models refuse `none`. Each request logs its tokens as `model usage` |
| `BOT_DISPLAY_NAME`            | `Maria`                         | The character's name                                                                                         |
| `PERSONA_PROMPT`              | built-in                       | The character's personality and rules; `{name}` is replaced by the name. See [The character](#the-character) |
| `MAX_HISTORY_MESSAGES`        | `20`                           | Recent messages sent to the model as context: from a quarter fewer to a quarter more, the start moving in steps so the prompt's beginning stays cached |
| `MAX_TOOL_CALLS_PER_TURN`     | `4`                            | Tool calls allowed while answering one message                                                               |
| `MAX_CONCURRENT_REQUESTS`     | `10`                           | Number of workers                                                                                            |
| `USER_RATE_LIMIT_PER_MINUTE`  | `10`                           | Messages each user may send per minute                                                                       |
| `REQUIRED_CHANNEL` | — | Channels, each its `@username` or `-100` ID, separated by commas, that a user must have joined to be answered in a private chat; others get a button for each and a «✅ Tasdiqlash» button that sends `/start check` to check again. Owners need not join. The bot must be an admin of the channel to check: until it is, nobody but the owners is answered; when Telegram does not answer at all, the message is |
| `PRIVATE_DAILY_LIMIT` | `10` | Answers each user gets per day in private chats, from midnight in `GROUP_HISTORY_TIMEZONE`; the next message gets one notice, later ones nothing. Owners have no limit (admins do); commands and failed turns are not counted; `0` turns it off. Counted in the database |
| `PREMIUM_PRICE_STARS` | `0` | Price of [premium](#premium) in Telegram Stars; `0` sells none. Off by default, so that no deploy starts taking money unasked |
| `PREMIUM_DAYS` | `30` | Days one purchase of premium lasts; buying again before it ends adds them to its end |
| `PREMIUM_DAILY_LIMIT` | `50` | Answers a day in private chats while premium lasts; premium is sold only when this is above `PRIVATE_DAILY_LIMIT` |
| `BUSINESS_DAILY_LIMIT` | `30` | Answers each customer gets per day from an owner's [business account](#telegram-business), from midnight in `GROUP_HISTORY_TIMEZONE`; past it, nothing, not even a notice. Failed turns are not counted; `0` turns it off. Counted in the database |
| `PRIVATE_CHATS` | `on` | Whom she answers in private chats: `on` everyone, `owners` the owners only, `off` nobody, so that she works in groups only. A private message that is not answered gets only a friendly reaction picked at random in code, never a notice, and the model is not asked. A group smaller than `GROUP_MIN_MEMBERS` counts as a private chat here, so that a small group cannot get around it; there, someone who is not answered gets only a 👀 reaction, and no call is made to the model, not even to transcribe a voice message. With `off`, owners cannot use private-chat tools such as broadcasts either |
| `REQUEST_TIMEOUT_SECONDS`     | `45`                           | Time budget for answering one message                                                                        |
| `MAX_INPUT_CHARS`             | `4000`                         | Longest user message accepted                                                                                |
| `RETENTION_DAYS` | `1` | Chat history is kept this many days, then deleted; `0` keeps it forever |
| `ADMIN_USER_IDS`              | empty                          | Comma-separated Telegram user IDs of administrators                                                          |
| `ADMIN_COMMANDS`              | empty                          | Comma-separated commands only administrators may use, e.g. `model,about`                                     |
| `ENABLE_WAKE_WORD_TRIGGER`    | `true`                         | In groups, answer when called by name; needs privacy mode off, see [Groups](#groups)                         |
| `BOT_WAKE_WORDS`              | the display name               | Comma-separated names the bot answers to, each one word, e.g. `maria,mariya,мария,мариа` (not `mira`: another bot has that name) |
| `ENABLE_USERNAME_MENTION`     | `true`                         | In groups, answer when `@username` is mentioned                                                              |
| `ENABLE_REPLY_TRIGGER`        | `true`                         | In groups, answer replies to the bot's messages                                                              |
| `GROUP_AMBIENT_MESSAGES` | `500` | How many of the last 10 hours' group messages are given to the model with a question (0 to 500; 0 turns the group log off) |
| `GROUP_HISTORY_TIMEZONE` | `Asia/Tashkent` | The groups' time zone: the times in the group log and in what the model is told, the day the picture and private-chat limits count, and the time given when nobody names a zone |
| `BOT_OWNER_USERNAME` | empty | The owners' Telegram usernames, comma-separated (e.g. `pronium,TursunoffM`); the bot treats owners warmly, follows their instructions and lets them manage it in any group |
| `BOT_OWNER_IDS` | empty | The owners' numeric Telegram user IDs, comma-separated; when set, only these accounts are owners and `BOT_OWNER_USERNAME` (then required) only names them. Safer: a username can be given up and taken by someone else |
| `ALLOWED_BOTS` | empty | Other bots the bot answers in groups, comma-separated usernames, or `*` for any bot; empty answers no bots |
| `BOT_REPLIES_PER_10_MIN` | `5` | Most replies to bots per group in 10 minutes, so two bots cannot talk without end |
| `GROUP_CONTEXT_MODE`          | `shared`                       | Only `shared` is supported: one conversation per group                                                       |
| `MAX_GROUP_REPLY_LENGTH`      | `4000`                         | Longest answer posted in a group (at most 4096; an emoji usually counts as two)                              |
| `GROUP_RATE_LIMIT_PER_MINUTE` | `6`                            | Answers per group per minute, across all members; past it she is silent. Counted in the database             |
| `MEMBER_REPLIES_PER_10_MIN`   | `4`                            | Answers each member gets per group in 10 minutes; past it she is silent with them. Counted in the database    |
| `GROUP_MIN_MEMBERS` | `6` | The smallest group whose members she answers, herself counted and other bots seen there not; a smaller group is treated as a private chat under `PRIVATE_CHATS`. 0 means no minimum |
| `GROUP_MIN_ACTIVE_MEMBERS` | `0` | How many people must have written in a group in the last `GROUP_ACTIVE_DAYS` days for the bot to answer there; in a quieter group it answers only its owners, silently passing over everyone else. Counted from the members the bot has seen. `0` turns it off |
| `GROUP_ACTIVE_DAYS` | `3` | The days `GROUP_MIN_ACTIVE_MEMBERS` counts back |
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

### Changing settings from Telegram

The owners can change the group, private-chat, premium, picture, trigger,
history and output settings without a deploy. `/settings` lists them with their values
and where each comes from; `/set NAME value` changes one and
`/set NAME default` goes back to the environment's. In a private chat it
changes the bot's; in a group (`/set@Bot …`) the group's own, for the four
that may differ by group: `GROUP_RATE_LIMIT_PER_MINUTE`,
`GROUP_MIN_ACTIVE_MEMBERS`, `GROUP_ACTIVE_DAYS` and `MAX_GROUP_REPLY_LENGTH`.
`/limit` and `/active` are short for the first two. A value is checked as the
environment's would be, with an upper bound for the numbers, before it is
saved in `settings`. A group's change holds at once; the bot's, within a
minute, as each serverless instance sees the change and rebuilds (long
polling needs a restart). The list is `config.Settings`; secrets, models and
keys stay in the environment only.

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

Bot commands: `/start`, `/help`,
`/model`, `/about`, `/premium`, `/paysupport`, with `WEBAPP_URL` set
`/stats` and `/game`, for owners `/payments` and `/refund`, and
in groups `/mute`, `/unmute`, `/ignore` and `/unignore`.

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
tools and answer within seconds, which matters for the
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
Only the person themselves can set it, or an owner replying to their
message (or to Maria's answer to them) with "buni Ustoz deb chaqir"; the
name must be in the asker's own message, and nobody else's turn ever shows
it. Nothing they said is kept;
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
| PDF, Word (.docx), text file (.txt, .md, .csv, .json, .log, .srt) | the file itself: a PDF as it is, which Gemini reads page by page; the text of the others, cut to 30,000 characters. Up to `MAX_MEDIA_MB`. The backup model gets only the file's name |
| Other file, song, poll | its name; the song's performer and title; the poll's question and options |

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

Several pictures in one answer, such as two heroes' portraits or a picture
drawn twice, go as one album (Telegram's `sendMediaGroup`, up to 10 a
time), with the answer as the caption under the first, instead of one
message each. An album Telegram refuses, as it does when it cannot read one
of the pictures, goes picture by picture; one it does not answer may be in
the chat, and is not sent again.

## The owner

Set `BOT_OWNER_USERNAME` to your Telegram username (with or without `@`);
for several owners, separate them with commas. The bot then knows you made
it: it is friendly to you, does what you ask in
the chat within its limits, and lets you use `/mute`, `/unmute`,
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

An owner with Telegram Premium can connect the bot to their own account as
its chatbot, for it to answer their customers, post their stories and send
them checklists; see [Telegram Business](#telegram-business).

Owners can also ask the bot about its own data ("Maria, nechta guruhdasan?"),
which it looks up with the owner-only `bot_database` tool:

- `overview`: how many groups it has been in, users, and today's messages and
  pictures.
- `groups`: every group it knows, with whether it is still there, whether it
  is an admin there and the member count, all checked live with Telegram.
- `messages`: today's messages of one group. Another group's messages are
  given only in a private chat with the owner, so they never land in a
  different group.

Owners can make photos, and pictures she draws, stickers in her own sticker
set with the owner-only `sticker_set` tool ("Maria, shuni stiker qil"); see
[Her own sticker set](#her-own-sticker-set).

Owners can have the bot send a member a Telegram gift, bought with the
bot's own Stars, with the owner-only `send_gift` tool ("Maria, unga sovg'a
yubor"), offered only when `BOT_OWNER_IDS` is set: it spends money, and a
username can change hands. `list` gives the gifts on sale with their price
and the bot's Stars balance (`getAvailableGifts`, `getMyStarBalance`);
`send` sends one (`sendGift`) with an optional text of up to 128 characters.
It goes, in a group, to the member the owner replied to or tagged with @,
confirmed with Telegram as the `moderate` tool confirms a tag, never to a
user the model names; the owner's own words must ask for a gift ("sovg'a",
"подарок", "gift"); and the balance is checked first, so a gift it cannot
pay for is refused with how many Stars are missing. Stars reach the balance
from premium and can be bought for the bot in @BotFather.

Owners can also have the bot post in its groups ("Maria, guruhlarga master
qaytdi deb yoz") with the owner-only `send_to_groups` tool. It works in two
steps: the bot first shows the exact text and the groups, and sends only after
the owner says yes in a later message, within 10 minutes. The code refuses a
confirmation made in the same message as the draft, so the model cannot send
on its own.

A broadcast goes to every group the bot is in unless the owner says
otherwise: to its channels instead ("kanallarga yoz"), to both, or to the
groups and channels they pick (`targets` lists them with their IDs). A
channel counts only while the bot is an admin there with the right to post
messages. Instead of text, an owner may reply to a message (a photo, a
video, a post, in the private chat or a group) and ask for it to be sent
("shuni hamma guruhlarga yubor"): it is copied as it is, with Telegram's
`copyMessage`, so it carries no "forwarded from" line, after the same yes.
Neither the text nor a copied message may call or tag someone to summon
them.

Groups are recorded when the bot is added, promoted or removed (Telegram's
`my_chat_member` updates) and from the messages it logs. Telegram has no way
to list a bot's groups, so a group the bot joined before this, where nobody
has written since, is not known. After upgrading, run `set-webhook` again so
Telegram starts sending `my_chat_member`. The
`groups` table keeps only each group's name and is not expired; messages
still are. Channels come only from `my_chat_member` (the bot never asks for
channel posts) and are kept apart, in `channels`, so that the overview, the
statistics and `bot_database groups` stay about groups; a channel the bot
was made an admin of before this upgrade is recorded once its rights change,
for example by removing and adding it again.

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
| `send_location` | Where a town, city, district or region is ("Samarqand qayerda", "Xiva lokatsiyasini tashla", "где находится Бухара"), sent as a pin on the map (`sendVenue`) under its name, with its coordinates as the address, to the chat the turn is in: in a group as a reply to the request, which keeps it in a forum topic. Found the way `get_weather` finds places, so only settlements and areas: never a street, shop or building, nor a pin guessed from memory. Asking for a location or a map ("lokatsiya", "xaritada", "где находится") skips the router | GeoNames places (CC BY 4.0), then the Open-Meteo geocoder |
| `prayer_times` | Prayer and Ramadan times | Aladhan; approximate, the Muslim Board of Uzbekistan (muslim.uz) is official |
| `news_headlines` | Latest headlines in Uzbek or Russian; with `world`, world news in Uzbek (Cyrillic), Russian or English | RSS feeds of kun.uz, gazeta.uz, uza.uz and daryo.uz, and of BBC O'zbek, BBC Russian and BBC World; headlines and links only |
| `wiki_lookup` | Article summaries, "on this day" | Wikipedia (CC BY-SA) |
| `football` | League tables, the current matchday's matches, top scorers | football-data.org free tier; scores can lag, so "latest", not live |
| `crypto_price` | Coin prices in dollars and sums | CoinGecko |
| `mlbb` | Mobile Legends: the meta by win, ban or pick rate; heroes by role and lane; a hero's profile, skills, base stats, rates and tier, with its counters, partners and the game's tips, and its portrait as a photo when asked; the most used build of top players by lane and rank, sent as one picture of its spell, emblem, talents and items, the boots and last two items taken from what the 30 hottest player guides add most; a hero's skins with tier, date and price, and a skin's picture; an account's nickname and country from its ID and zone | MLBBDex (mlbbdex.com, from the Mobile Legends Wiki) first: heroes, profiles, skins and the rates of all ranks, measured about weekly. Rone Arena API, a fan-made, keyless mirror of Moonton's MLBB Academy, for what MLBBDex lacks: rates by rank, counters, partners, tips, builds and player guides, cached for 3 hours, and the profile when MLBBDex is down. api.isan.eu.org for accounts, which answers a wrong ID and its own failure alike. All unofficial |
| `web_search` | Current facts from the web: an answer and up to five results with their sites, or news of the last 7 days | Tavily, with `TAVILY_API_KEY` (free: 1,000 searches a month); without the key there is no web search |
| `movies` | Films and series: search by title, a title's plot, genres, length, director and cast, and what is trending or popular in a genre | TMDB, with `TMDB_TOKEN`; plots in Russian or English, as TMDB has few Uzbek ones. This product uses the TMDB API but is not endorsed or certified by TMDB |
| `youtube_search` | YouTube videos: guides, highlights, trailers, full songs, with channel, length, views and a youtu.be link that Telegram plays | YouTube Data API, with `YOUTUBE_API_KEY` (an `AIza…` key restricted to YouTube Data API v3; free: 10,000 units a day, a search costs 101); results cached for 2 hours |
| `music_search` | Songs by title or artist with their links; a 30-second preview of the first match, named after the song, is sent as audio and is her whole answer | Deezer, keyless; Deezer gives only previews |
| `quran` | Quran verses by reference (up to 7 at once) with Muhammad Sodiq Muhammad Yusuf's Uzbek translation in Latin script, or Kuliev's Russian or Saheeh International's English; the verse of the day; a surah's facts; a search of the Uzbek meaning | quran.com, keyless; the search uses alquran.cloud, whose Uzbek text is Cyrillic, so a Latin query is written in Cyrillic first; cached for a day |
| `fun` | One tool for small amusements: a trivia question with shuffled options and its answer (by category and difficulty), a random fact (facts about sex, drink or drugs are skipped), a piece of advice, a famous quote, a dog picture | Open Trivia DB, Useless Facts, Advice Slip, ZenQuotes and Dog CEO, all keyless; gathered under one schema so that they cost the prompt one tool |
| `more_info` | One tool for more look-ups: books by title or author, recipes by the dish's English name (pork and alcohol left out), the world gold and silver price per ounce and gram, Lichess's daily puzzle and a player's ratings, this Formula 1 season's standings and the last race | Open Library, TheMealDB, gold-api.com, Lichess and Jolpica (the Ergast API's successor), all keyless; cached for an hour, metal prices for 10 minutes |
| `study` | For students: scholarly articles from Crossref with DOI links, arXiv preprints with PDF links, and English synonyms, antonyms, rhymes and words of like meaning | Crossref, arXiv and Datamuse, all keyless; one schema; papers and words cached for 6 hours |
| `holidays` | A year's holidays and days off from Calendarific, each with the days left until it: all of Uzbekistan's, working Saturdays and Ramadan included; another country's national holidays only. Equinoxes and solstices are dropped | Only with `CALENDARIFIC_API_KEY`; cached for a day |
| `make_meme` | A meme from one of Imgflip's hundred popular templates (Drake, Distracted Boyfriend, Two Buttons...), the texts in Noto Sans so that Cyrillic shows, sent as a picture. A template used in a chat's last four memes is refused there, with others offered, unless the asker names it, so the model does not draw the same few (it drew Drake in ten of nineteen). Only templates: a member's own photo would become a public template on a free plan. Needs `IMGFLIP_API_KEY` |
| `read_page` | A web page's title and text, cut to 8,000 characters, to summarize a link or answer about it | Jina Reader (r.jina.ai), keyless (about 20 pages a minute); local addresses and Telegram's file links are refused; cached for 30 minutes |

The tools share one HTTP client with short timeouts and a small in-memory
cache, send `MariaBot/1.0 (+https://t.me/<bot username>)` as the user agent,
and need no key except football. Text from outside sites comes back marked as
untrusted in the tool descriptions.

For data and longer explanations she writes Markdown: `**bold**`, "- " lists,
"## " headings and tables. An answer with headings, lists, tables or
quotations goes as a rich message (`sendRichMessage`, Bot API 10.1), which
Telegram renders with large headings and real lists; if Telegram refuses it,
it goes as ordinary text. Ordinary messages are sent without a parse mode: the
code turns `**bold**` and `` `code` `` into entities itself, so a stray mark
stays as text and never makes a send fail.

## Daily limit in private chats

In a private chat each user gets `PRIVATE_DAILY_LIMIT` answers a day
(default 10), counted from midnight in `GROUP_HISTORY_TIMEZONE`.

- Counted: every message that reaches the model, photos and voice messages
  included, and one the model chose not to answer. Not counted: commands, a
  message too long or of a kind she cannot read, a message Telegram delivers
  twice (it counts once), and a turn that failed with an error (the model
  overloaded, out of time, or any other error), unless she drew a picture
  before the error: the picture is still sent, and counted.
- The limit is for talk with the model; tools take nothing from it. A turn
  that called a tool (a rate, the weather, a search, a meme, a picture...)
  is given back once it is answered. Past the limit, a message in words
  alone is still a turn, for tools only (`agent.Request.ToolsOnly`): the
  router, a cheap call, sends talk straight to the limit's notice, and an
  answer the main model gives without a tool is dropped, unsaved, for the
  notice too. A picture or a voice message past the limit is neither seen
  nor transcribed. Each tool keeps its own limits, as drawing does.
- The first message past the limit that is not a tool's gets one notice, in
  Uzbek, Russian and English; later ones that day get nothing, but while
  premium is sold, a line pointing to `/premium` at most once an hour.
- Owners have no limit. Everyone else has, the administrators in
  `ADMIN_USER_IDS` included. Groups are not affected; they have their own
  [limits](#limits).
- The count is kept in the database (`private_messages`: the user's ID and
  the time of each answer, never the text), so it holds on Vercel and across
  restarts. When it cannot be read she answers anyway and logs a warning:
  the limit guards the model's cost, and one answer too many disturbs
  nobody else.
- `0` turns the limit off.

### Keyboard and message effects

`/start` and `/help` in a private chat bring a small keyboard, in place of the
phone's own, of her common requests, in the language of the user's app
(`internal/service/keyboard.go`): «💵 Valyuta kursi», «🌤 Ob-havo», «🎮 MLBB»,
«🎲 Zar tashla», «🕐 Soat nechi?» and «❓ Yordam». A meme has no button, which
the owners took off: it is asked for in words, as before. Telegram keeps it there, shrunk to fit,
until it is closed. A button sends its own text, in any of its three
languages, and the buttons that need a value ask for it first
(`internal/service/keyboardmenu.go`):

- «💵 Valyuta kursi» asks which currency, with buttons for USD, EUR, RUB,
  KZT, CNY and GBP; a press gives its Central Bank rate, with the reply box
  open for an amount, and the amount written there ("1 500", "2,5") is
  converted to so'm.
- «🌤 Ob-havo» asks which city, with buttons for eight of Uzbekistan's and
  one for another, which opens the reply box for its name; a name the
  weather does not know is said to be unknown.
- «🎲 Zar tashla» asks which game: 🎲 🎯 🏀 ⚽ 🎳 or 🎰.
- «🎮 MLBB» opens a menu of Mobile Legends: 🔥 Meta, 🦸 Qahramon, 🛠 Build,
  ⚔️ Kontr-pik, 👕 Skinlar, 🎯 Draft, 📈 Win rate and 🆔 O'yinchi. The meta
  is asked of the model at once, as if written in reply to the menu (its own
  update, the press standing in for its ID); the rest open the reply box
  for the hero, the enemy's picks, the matches and win rates, or the
  player's ID and zone, and the answer goes to the model with the request
  ("MLBB: Ling uchun eng yaxshi build"), which the `mlbb` tool answers.
- «🕐 Soat nechi?» is answered at once, by the bot's clock.

A choice comes back as a button's data (`kb:rate:EUR`), handled at once; a
value written in the reply box is known by the question it replies to, so
nothing is kept between the question and its answer. The answer that ends
a question brings the keyboard back: the reply box hides it and keeps the
question's hint ("Masalan: Ling") in the box, which people took to mean
that a hero's name was still wanted; a picture, such as a skin, brings it
too (an album of several cannot). A question is asked in the language of
the label pressed, «🎲 Roll a die» in English, as a few words are no sure
sign of a language; «🎮 MLBB», the same in every language, in the user's app's. All of it but
Mobile Legends is answered in code (`internal/service/keyboardanswer.go`), with the
tools run by their registry on the sender's turn, and takes nothing from
the daily limit, past it too; a tool that fails is said to have failed. A
Mobile Legends request is a turn of the model's that calls a tool, so it is
not counted either (below). A chat that still has an earlier keyboard,
«💵 Dollar kursi» or «🌤 Toshkent ob-havosi», gets the same questions.
«❓ Yordam» (or «❓ Помощь», «❓ Help») is answered as `/help`, uncounted.

The same plain requests typed in words are answered in code too
(`internal/service/typedrequest.go`), in a private chat: a message that is
nothing but a currency's rate ("dollar qancha", "evro kursi", "курс
рубля"), the weather, of one named place or Tashkent ("ob-havo", "Samarqand
ob-havosi", "погода"), or the time ("soat nechi", "который час"), word by
word, five words at most. Anything more, an amount to convert, another day,
a second question, goes to the model, and so does a place the weather does
not know. A group never gets the keyboard, and the buttons' words typed
there are just words, for the model; so are they in a business account's
chats.

An answer in a private chat that congratulates someone, a birthday, a
holiday, an exam passed ("tabriklayman", "tug'ilgan kuning bilan",
"muborak", "поздравляю", "happy birthday"), comes with Telegram's 🎉
message effect. The words decide, in code: the model never names an effect,
so it can never ask for one Telegram does not have. Telegram plays effects
only in private chats, so groups never get one, and a message Telegram
refuses with its effect is sent again without it.

## Inline mode

Typed in any chat, `@MariaBot qanday qilib plov pishiriladi?` shows one
result, Maria's answer, which the asker taps to post it there as their own
message ("via @MariaBot"). Telegram gives the bot the words and the asker,
never the chat.

- **Turn it on** in [@BotFather](https://t.me/BotFather): `/setinline`,
  choose the bot, and give the placeholder shown in the input field, such
  as `savol bering…`. Until then Telegram sends no inline queries.
- **A question must end** in `?`, `!`, `.` or `…` to be answered. The app
  sends a query at nearly every keystroke; answering each would spend a
  model call, and one of the asker's daily answers, on every unfinished
  word. Until then, and for an empty query, the result is a hint.
- **It is a private chat's turn, without the chat**: the same daily answers
  (`PRIVATE_DAILY_LIMIT`, owners unlimited; past them the result says so),
  `PRIVATE_CHATS` and `REQUIRED_CHANNEL` apply, and the flood guard. The
  persona and the owner's notice are the same, but there is no history, no
  tools and nothing is saved: the light chat model (`CHAT_MODEL`, the main
  model without `ROUTER_MODEL`) answers alone, told that its answer is
  posted by the asker in a chat it cannot see. A question that needs a tool,
  such as the weather or a rate, gets a result saying to ask in a private
  chat.
- **Telegram waits about ten seconds.** The checks and the answer get 7
  seconds; past them, or when the model fails, the result says to try
  again. An answer is kept by Telegram for 30 seconds, for that person only
  (`is_personal`); a failure is not kept, so trying again asks again.
- Inline queries come only when the bot asks Telegram for them
  (`inline_query` in `allowed_updates`): long polling asks on every
  request, and the webhook function sets the webhook again at its first
  start after this update when the kinds differ, as `set-webhook` does.

### Premium

Users can buy a higher limit with Telegram Stars (currency `XTR`, no payment
provider): `PREMIUM_PRICE_STARS` Stars for `PREMIUM_DAYS` days of
`PREMIUM_DAILY_LIMIT` answers a day. It is sold only while the price is
above 0, `PRIVATE_DAILY_LIMIT` is on and `PREMIUM_DAILY_LIMIT` is above it;
all three can be changed with `/set`.

- **The offer.** `/premium` in a private chat says what premium gives, or
  until when the sender has it, and sends the invoice (`sendInvoice`), whose
  Pay button opens Telegram's payment. The day's one notice that the limit
  is reached carries the same offer and invoice, so it comes once a day at
  most and only when it is wanted; someone who has premium is told the
  premium limit and offered nothing. After it, a sender without premium
  who writes on is reminded "Bugungi limit tugadi. Ko'proq yozish uchun
  /premium ni bosing." at most once an hour (counted in each instance's
  memory), as one who missed the notice saw only a reaction. In a group `/premium` points to the
  private chat.
- **The check.** Before taking the money Telegram asks the bot
  (`pre_checkout_query`), which must answer within 10 seconds. The answer is
  made from the invoice's payload alone, `premium:<user ID>:<days>:<price>`:
  yes when it is the plan on sale now, in Stars, paid by the user it was made
  out to; otherwise no, with the reason shown to the payer (an invoice from
  before the price or the days changed, or one forwarded from someone else).
- **The payment.** Telegram then posts a `successful_payment` service
  message. The payment is stored in `star_payments` by its
  `telegram_payment_charge_id`, so a payment delivered twice counts once, and
  its days are added to `premium_users.until`: to the end of a premium that
  still lasts, from now otherwise. The payer is thanked with the end date.
  This works even with `PRIVATE_CHATS=off`: the money moved either way. A
  payment that does not match its invoice is kept with 0 days, for a refund,
  and logged as an error; one that cannot be stored is logged with its
  charge ID, and the payer told that the owners will sort it out.
- **The limit.** While `until` is in the future the daily limit is
  `PREMIUM_DAILY_LIMIT`, looked up in the same transaction that counts the
  answers. Premium already bought still counts if sales are turned off.
- **Owners.** `/payments` in a private chat lists the last 20 payments with
  the payer, Stars, days and whether they were refunded. `/refund <user ID>`
  or `/refund @username` gives the user's latest payment back
  (`refundStarPayment`) and ends their premium at once; a payment Telegram
  had already given back is only recorded. A refund Telegram reports by
  itself (`refunded_payment`) ends the premium too. Nobody else can use
  either command, and neither works in a group.
- `/paysupport`, which Telegram asks of every bot taking Stars, tells
  users to write to the owners, by their usernames, about a payment.
- Payments are never deleted by the daily cleanup.

## Telegram Business

An owner with Telegram Premium can connect Maria to their own account as
its chatbot. She then answers the customers who write to that account, on
its behalf, and, asked in her private chat, posts the account's stories and
sends its customers checklists.

**Connecting her.** In Telegram: Settings → Telegram Business → Chatbots,
add the bot by its `@username`, choose which chats it may answer (all
private chats, or some, with exceptions), and in its settings turn on
replying to messages and, for stories, managing stories. Telegram reports
the connection (`business_connection`); Maria records it in
`business_connections` and tells the owner, in her private chat, what she
may do there and what she lacks. Disconnecting her, or changing what she may
do, is reported and recorded the same way.

- **Owners only.** Only an account whose user is in `BOT_OWNER_IDS` is
  used: the account speaks for whoever holds it, and a username can change
  hands, so `BOT_OWNER_USERNAME` alone is not enough. Anyone else's
  connection is recorded with `refused`, logged (`a business account that is
  none of the owners' connected the bot; ignoring it`), and never answered
  through.
- **A connection she has no record of**, as one made before this version,
  is asked of Telegram (`getBusinessConnection`) at its first message, and
  recorded.

**Answering customers.** A customer's message to the account comes as a
`business_message`, in the account's private chat with them. Maria answers
it as she answers a private chat, with the persona, the conversation's
history and the tools, and sends the answer with the connection's
`business_connection_id`, so that it comes from the account. Each turn
tells the model whose account it speaks for: to be the owner's warm, brief
assistant, and to leave prices, orders, payments, meetings and anything
else only the owner can promise to the owner. Every message there, the
owner's too, is noted in `business_chats`, the customer's name and when the
owner last wrote.

- **Never the owner.** The owner's own messages in the chat are never
  answered, nor messages a bot sent for the account (her own answers among
  them, which Telegram also reports), another bot's, a service message, or
  another owner's: owners talk to her in her own chat. When the owner wrote
  in a customer's chat in the last 15 minutes she leaves it to them: two
  answering at once, she without knowing what the owner said, would be
  worse than either alone.
- **Only where she may.** Telegram sends her only the chats the owner gave
  her, and she answers only while the connection is enabled, lets her
  reply (Telegram allows it in chats with an incoming message in the last
  24 hours) and the owner has auto-reply on.
- **Apart from her own chats.** The conversation is kept under the owner
  (see [Database schema](#database-schema)), never mixed with the
  customer's own private chat with Maria, and the turn has none of that
  chat's extras: no owners' notice or standing rules, no profile, no talks
  in other chats. The tools that send to the chat themselves, as the bot
  rather than the account, or keep what they learn of the person for their
  own chats (`send_dice`, `my_profile`) are not offered (`NoBusiness`), nor,
  as ever, the group and owner tools.
- **Words and pictures only.** Text and pictures (drawings, memes, albums)
  go through the account. A reaction, sticker, GIF, file or song does not,
  and an answer that is only one of those sends nothing; the model is told
  so. A command such as `/help` is only text for the model: the bot's
  commands are for its own chats.
- **Limits, silently.** The flood guard (`USER_RATE_LIMIT_PER_MINUTE`)
  applies, and each customer gets `BUSINESS_DAILY_LIMIT` answers a day
  (default 30), counted per customer chat in `business_replies`. No notice
  ever goes to a customer from the owner's account: past a limit, when the
  workers are busy, or when the turn fails (an error, a timeout, an
  overloaded model), she sends nothing, and the failed turn's answer is
  given back. Skips are recorded as in other chats (`business_limit`,
  `flood`).
- **No typing, no read marks.** She does not show "typing" or mark
  messages read in the account.

**/business**, in an owner's private chat with her, shows their business
account: whether it is connected, whether she may reply, whether auto-reply
is on, and whether she may post stories, or how to connect her when it is
not. `/business off` stops her answering its customers, `/business on`
starts her; it holds for all the owner's connections, and a new
connection takes the owner's last choice. An owner's first connection has
auto-reply off: she would speak for them to people they know, so she
answers nobody until they say `/business on`. For anyone else, or in a group,
it is an unknown command.

**Stories.** "Maria, buni storyga joyla" with a photo, or in reply to one,
or "chizib storyga qo'y", has the owner-only `post_story` tool post a story
on the owner's business account (`postStory`, which needs *manage stories*).
The picture is the one drawn or made in the same turn (`generate_image`,
`make_meme`), or else the photo the owner sent or replied to; a sticker
works too. Telegram takes a story only as a new 1080×1920 upload of at most
10 MB, so the picture is fitted whole in the middle, on itself blown up to
cover the story and darkened, and sent as a JPEG. An optional caption, up to
2048 characters, and how long it stays up, 6, 12, 24 (the default) or 48
hours. It works only for an owner, only in their private chat with her, and
only on their own account, the connection recorded for their ID; the model
names no account.

**Checklists.** Telegram lets bots send checklists only on behalf of a
business account. The owner-only `business_checklist` tool, in the owner's
private chat:

- `chats` lists the customer chats she has seen through the account, the
  latest first.
- `send` sends a checklist, a title (up to 255 characters) and 1 to 30
  tasks (up to 100 each), into a customer's chat (`sendChecklist`), named by
  the customer's first name, `@username` or chat ID; a name two customers
  share needs the ID. Both the owner and the customer can tick its tasks
  and add more. It is kept in `business_checklists`.
- `edit` replaces the title and tasks of the last checklist she sent into
  that chat (`editMessageChecklist`). Bots cannot tick tasks: the owner and
  the customer do that in the chat.

Like an answer, a checklist can go only to a chat with an incoming message
in the last 24 hours, and needs the right to reply; Telegram's refusal is
passed to the owner.

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

Each start sets the menu Telegram opens on "/" (`setMyCommands`, in
`internal/app/commands.go`): `/start`, `/help` and
`/about` in private chats; `/help`, `/model` and `/about` for a group's
members; and for its admins also `/mute`, `/unmute`, `/ignore` and
`/unignore`. In a group the menu adds the bot's username itself. A failure is
only logged.

Each start also sets the admin rights she asks for when someone makes her an
admin (`setMyDefaultAdministratorRights`, `groupAdminRights` and
`channelAdminRights` in the same file), which Telegram's dialog then shows
ticked: in groups the ones her features use, *Delete messages*, *Ban users*,
*Pin messages*, *Invite users*, *Manage topics*, *Change group info* and *Add
new admins*; in channels *Post messages* and *Edit messages*, for the owners'
broadcasts. Whoever adds her can still untick any of them, and she only ever
uses a right both she and the admin asking hold. A failure is only logged.

Each start also sets the profile's bio and description (`internal/app/profile.go`),
in Uzbek for everyone and in Russian and English for those apps, where they differ
from Telegram's. Change them there, not in @BotFather or through the bot: the next
start puts them back. Owners can still change the bot's name and photo by asking it.

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
`/help` is ignored, because it may be meant for another bot.

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
  10 hours are given to the model as context, marked as untrusted, with
  times in `GROUP_HISTORY_TIMEZONE`; of her own lines only the last 5,
  shortened. The log is deleted with the rest of the history after
  `RETENTION_DAYS` and when the group is muted. Nothing is
  logged while a group is muted. Set `GROUP_AMBIENT_MESSAGES=0` to turn the
  log off.
- **Standing rules:** owners' orders and the ignore list are kept in the
  `group_rules` table until they expire, whatever `RETENTION_DAYS` says; an
  owner's form of address until it is removed. `/mute` does not
  touch them.
- **Moderation:** what she did, and for whom, is kept in the
  `moderation_actions` table until you delete it, including up to 500
  characters of each message she deleted.
- **The quoted message:** when someone replies to a message while addressing
  her, that one message is included, shortened to 1,000 characters. It is
  saved as part of the question.
- **Never mixed:** a private chat's history is not visible in any group, and
  one group's history is not visible in another.

Because a group's conversation is shared, what one member says to her can
come back in her answer to another member of the same group.

### Muting and ignoring

| Command       | Who          | Effect                                                                              |
| ------------- | ------------ | ----------------------------------------------------------------------------------- |
| `/mute@Bot`   | group admins | She stops reacting in this group, and stops keeping recent messages, until unmuted. |
| `/unmute@Bot` | group admins | She is back.                                                                        |
| `/ignore@Bot`, as a reply to a member's message | group admins | She does not answer that member, or react to them, for 24 hours. |
| `/unignore@Bot`, as a reply to a member's message | group admins | She answers them again. |

"Group admins" are the group's owner and administrators as Telegram reports
them, plus anyone in `ADMIN_USER_IDS` and the bot's owners. The mute setting
is stored in the database and survives restarts. `/mute` leaves
the standing rules alone. Admins, owners and the bot itself cannot be
ignored.

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
- While she is working on an answer the chat shows her as typing, once
  the answer has taken two seconds: a quicker one, which is what a reaction
  usually is, comes without it, so "typing" is never followed by an emoji
  alone.
- Telegram allows only its own list of reaction emoji; a group can allow
  fewer. A reaction Telegram refuses is logged at debug level and dropped.

### Standing rules and the ignore list

Owners' orders that should last ("don't talk about finance", "don't answer
Bek", "call me aka") are saved by the owner-only `group_rules` tool, which
the model calls when an owner gives such an order. They are kept in the
`group_rules` table, apart from the conversation, so `/mute`
does not erase them, and every turn in the group is told the ones in force.

| Rule | Who sets it, and where | How long |
|---|---|---|
| Instruction | an owner, in the group it is for, in their own words from that message or their own message it replies to (never words from someone else's message or the chat); at most 20 per group, 300 characters each. An order about one member ("uni", "bu odam") is saved with that member, and only when the owner replied to their message or to her answer to them | a week, or 1 to 720 hours if the owner says |
| Ignore a member | an owner replying to that member's message, or to her answer to them, in the group | 24 hours, or 1 to 720 hours if the owner says |
| Form of address | an owner, for themselves, in any chat; it holds everywhere | until removed |
| Swear guard | an owner, in the group it is for ("so'kinganlarni avtomatik mute qil", optionally "@Bakxtyorvch ni chaqir"); one per group | until turned off |
| Spam, link, picture and joins guards | an owner, in the group it is for (`guard` `spam`, `links`, `images` or `joins`; see [Join requests](#join-requests) for the last); the joins guard also the group's creator and its admins who may invite users, through the `join_captcha` tool | until turned off |
| Punishment | an owner or the group's creator, in the group, for swearing or nude pictures: in reply to a member ("bu bola yana so'kinsa ban qil") or for anyone ("18+ tashasa 3 soat mute qil"); one per member and kind | 30 days, or the hours given |

The swear guard runs in code, asking the model only one thing. In a group
where it is on, every message from a member is checked against a list of
swear words (dnx, pshnx, нахуй, бля, qo'tog', jalab, jala, kot, ske and the
like; words that mostly mean something harmless, such as "am" or "mol", are
left out). Only a message the list catches goes to the model, which answers
in one word what it is: a swear at the bot, at a person, an exclamation
aimed at nobody, or no swearing at all, as "кот" for a cat or "blan" for
bilan. An exclamation ("бля, yutqazdik", "pzds") gets one warning a day and
never a mute (action `swear_word`): muted for them, members were unmuted
within minutes by the admin the guard calls. A message the model cannot
judge is left alone. A member's first insult at a person gets a reply
warning them; the next one within 24 hours mutes them for an hour.
A swear at the bot gets no warning: the first is answered with one of the
owners' two clips, chosen at random and embedded in the binary
(`internal/service/clips`; both go as GIFs),
and the second within 24 hours mutes them for 15 minutes, with a clip, or,
when she cannot mute (an admin, or no right to), gets silence.
A mute notice tags the @username the owner named, or else the group's
creator. Telegram does not let an admin be muted, so an admin who insults a
person is warned the same way and the next insult only tags that person.
The creator, owners and bots are never touched, forwarded messages are not
checked, and a message the guard handled is not answered by the model. Each
caught insult is kept in `moderation_actions` (action `swear`, or
`swear_bot` for one at the bot), which is how the count is kept and how an
update delivered twice is handled once. Muting needs her to be an admin who may
restrict members; without that right the notice says so and still tags.
"Maria, endi so'kinishni kuzatma" turns it off. She cannot watch a chat
herself, and says so rather than promise to.

An edited message is checked again, by this guard and by the spam, link and
picture guards, as a new one would be (`edited_message` updates): a member
who edits a swear into a message, a malicious link, or a nude picture in
place of the first one, is warned, muted or has the message deleted all the
same. An edit of a message the swear guard already caught is no second
swear: fixing a typo in an insult does not mute. Nothing else is done with
an edit: the model never answers one, even one that calls her, it takes
nothing from the reply quotas, and the group log keeps the first version.

A punishment from an owner or the group's creator (the `punishments` tool,
kinds `swear_punishment` and
`picture_punishment`, "ban" or "mute:<hours>" in text) is carried out in code
too, so it holds whether or not anyone talks to her: words alone, kept as an
instruction, reached only the model, which never sees a message not meant
for it. One set for a member, by replying to their message, holds for any
swearing of theirs, exclamations too, in place of the guard's warning or
mute, and works with the swear guard off; one for anyone in the group holds
for insults only. A picture punishment follows the picture guard deleting a
nude picture, so it needs that guard on. The creator is checked with Telegram
(`getChatMember`) each time; admins cannot set one. The asker's own words
must ask for the ban or the mute, as with moderation. Owners are never punished, and an
admin, whom no bot can ban or mute, is named instead.

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

### Picture guard

Where the picture guard is on, every picture is judged in code once ever,
by Telegram's `file_unique_id`, which is the same for a file in every
chat: the verdict is kept in `picture_verdicts` (migration 0040), so a
sticker or meme sent again, in any group, on any instance or after a
restart, is never sent anywhere a second time, and the last 6 hours' are
also remembered in each instance's memory to spare the database. A new
picture goes to Sightengine when its keys are set. When they are not, or
Sightengine fails (out of its free plan's month it answers
`400: 30-day usage limit reached` for days), the bot's own model is shown
the picture with a short prompt and answers one word, `NUDE` or `SAFE`,
drawing Sightengine's line: nudity, sex and a pose or outfit meant to
arouse, photographed or drawn, are deleted; swimwear, a kiss or a cartoon
are not. It is `ROUTER_MODEL` when that is set (light and cheap, with the
main model and its fallbacks behind it when it is busy), else the main
model, with 1024 tokens and low thinking. After Sightengine fails it is
not asked for an hour, in that instance, and the model judges alone. A
picture neither can judge (the model down, blocked or answering something
else) is left alone and not kept, so that it is judged when it comes
again: the guard never deletes on a guess. Each verdict is logged as
`guards: picture checked` with `judge=sightengine` or `judge=model`, and
kept with its judge.

### Moderation

A group's admins can have her act on a message by replying to it and asking
("Maria, o'chir", "Maria, buni 30 daqiqaga mute qil", "Maria, pin qil"). The
model calls the `moderate` tool, which is offered in groups only, and every
check is made in code:

- **Actions:** delete the message; purge, which deletes every message its
  author wrote in the group in the last hour, or the minutes asked for, two
  days at most, since Telegram deletes nothing older ("Maria, buning
  hammasini o'chir", "tozala", "очисти", "удали все"); mute its author (an hour unless asked
  otherwise, at most a week; supergroups only) or unmute them; ban them, or
  unban; pin the message without a notification, or unpin it; take away a
  member's stickers and GIFs, or photos (a day unless asked otherwise), and
  give them back; make a member an admin, with the bot's own delete, mute,
  pin and invite rights at most (never the right to make admins), take it
  back or set their title, for admins the bot made. A member's other limits
  stay: one already muted or without stickers stays so, and what is given
  back keeps the rest of the restriction's end.
- **Purge** finds the messages in the group log (`group_messages`, by
  author and message ID), as the Bot API cannot list a chat's messages: with
  the log off it is refused, and what the log never saw, such as messages
  the guards deleted, is not purged. The message replied to goes too,
  however old. Telegram is asked to delete them a hundred at a time
  (`deleteMessages`), at most a thousand in one purge; it skips those it no
  longer has. A member who left or was banned can still be purged, which is
  how a spammer just banned is cleared out. It is one action in the audit,
  with the minutes and the number of messages, and like a deletion it can
  never be undone.
- **Posts on behalf of a channel** (a member writing as their channel; not
  the group posting as itself, as anonymous admins do, nor the linked
  channel's posts Telegram forwards): ban and unban act on the channel,
  with `banChatSenderChat` and `unbanChatSenderChat`, so that whoever runs it
  can no longer post as it in the group, under any account; the post itself
  may be deleted or pinned. Muting, stickers, photos, adminship and purge are
  for members, and are refused for a channel's post with that reason. The
  audit records the channel by its chat ID, and undo puts a channel's ban
  back as it does a member's.
- **The group itself,** with no reply or tag: rename it, set or clear its
  description, set its photo from the photo in the request or remove it,
  make an invite link (named, and ending when asked), unpin everything, and
  `lock` or `unlock` one kind of message for everyone (messages, photos,
  videos, stickers and GIFs, voice and round videos, files, music, polls,
  link previews), keeping the group's other permissions. Each needs its own
  admin right, the asker's and the bot's: changing the group's info,
  inviting users, pinning or restricting members.
- **The group's sticker pack** (`set_sticker_set`, `delete_sticker_set`,
  Telegram's `setChatStickerSet` and `deleteChatStickerSet`): replying to a
  sticker, "shu stikerlarni guruhga qo'y" gives the group that sticker's
  pack, which every member may then use there; the pack comes from the
  sticker in the update, never from a name the model gives. "stikerpakni
  olib tashla" takes it away. Both need *change the group's info*, the
  asker's and the bot's, words about stickers or a pack ("stiker",
  "paket", "стикерпак"), and a group Telegram lets have one: she asks
  `getChat` first and, where `can_set_sticker_set` is false (only large
  enough supergroups may), says so. Undo puts the old pack back, or none.
- **Invite links:** a new link may also admit only a number of people (1 to
  99999) or make each who opens it ask the admins to approve them; Telegram
  does not combine the two, so asking for both is refused with that reason.
  `revoke_link` stops a link she made from working and `edit_link` changes
  its name, end, member limit (0 removes it) or approval: the link the asker
  gives, or else the latest she made in that group. She acts only on links
  she made there, found in `moderation_actions` (the link and its limits are
  kept in the record's `undo`), never on the group's other links; the
  asker's words must name a link and say to revoke it ("bekor qil",
  "o'chir", "отзови") or what to change ("20 kishiga chekla", "tasdiq bilan
  qil"). Telegram sets every limit anew in an edit, so she sends the link's
  own limits with the one changed, and an end once set can be moved but not
  taken away. Undo revokes the latest link she made, edited or not; a
  revoked link is never revoked again.
- **Forum topics,** in a group with topics, through the separate
  `forum_topic` tool: create a topic (named, its icon in one of Telegram's
  six colours when asked), rename, close, reopen or delete the topic the
  request was written in (or that of the message it replies to, as Telegram
  says in the update: never one the model names), and hide or show the
  General topic. A request in General acts only on General itself. The
  asker and the bot both need *manage topics*, and *delete messages* too to
  delete a topic, which takes every message in it; the asker's words must
  name a topic ("mavzu", "topic", "тема", or "General" to hide or show it)
  and say what to do ("yop", "och", "o'chir", "yashir"). Topic actions are
  audited and counted like the others, as `topic_close` and so on, but undo
  does not put them back: a closed topic is reopened by asking.
- **Undo** ("orqaga qaytar"): every action but a deletion or a purge is recorded with
  what puts it back, read from Telegram just before it is taken
  (`moderation_actions.undo`). Undo puts back the group's latest such
  action, once; asked again, the one before. It needs the right the action
  needed. A name, description or lock someone changed by hand since is not
  overwritten unless they say so again. A restriction that has ended since
  is put back as none, never as forever. Deleted messages, and messages the
  guards deleted, never come back; an unban lets the member rejoin but does
  not add them; a demoted admin gets back only the rights the bot holds; and
  undoing "unpin all" pins back only the latest pinned message, the one
  Telegram reports.
- **No confirmation:** as the owners asked, the action is done at once,
  with no question to answer. Instead, the person's own message (or their
  voice message's transcript) must ask for it in words, checked in code:
  "o'chir", "mute qil", "jim qil", "ban", "blokla", "pin qil", "заблокируй",
  "закрепи" and the like; a purge needs words for all of them, "o'chir"
  alone deleting one message. A text in the chat or a quoted message can talk
  the model into calling the tool on someone's turn, but nothing is done
  unless that person asked for it themselves.
- **Target:** the message the person replied to, as Telegram delivered it.
  For mute, unmute, ban and unban, someone who replied to nothing or to
  their own message may instead tag one member: with a link to a member
  without a username, Telegram gives the ID; with an `@username`, the ID is
  the last author seen under it in that group's log, and Telegram must
  confirm that this account still has that username and is in the group.
  Without a tag they may name the member ("Azamatni mute qil"): the model
  passes the name, and it acts only on the one member seen in the group by
  that first name or username whose name is in the person's own words, as
  it is or with an ending such as -ni or -ga; a first name two seen members
  share needs their username. Two tags, an unknown username, a name not in
  their words or any mismatch act on nobody, and she asks for a tag or a
  reply to the member's message instead. Nothing the model, a member or a
  quoted message writes can choose whom she acts on beyond the person's own
  words. Her answers always reply to the person's own request.
- **Who may ask:** a bot owner recognised by `BOT_OWNER_IDS` (not by
  username alone), or the group's creator or an administrator
  who holds the matching right, as Telegram reports it at that moment:
  *delete messages* for delete and purge; *ban users* for mute, unmute, ban
  and unban; *pin messages* for pin and unpin.
- **Her rights:** she needs the same right herself, checked with Telegram
  each time. The model is told those of her rights in each group that a tool
  uses (delete, ban and mute, pin) so that it does not
  offer what she cannot do. Give her only the rights you want her to use.
- **Never a target:** the person asking (they may still have their own
  message deleted or pinned), the bot itself (its own messages may be
  deleted or pinned), the bot's owners recognised by `BOT_OWNER_IDS`, the
  group's admins, and Telegram's service accounts (a channel's post, which
  Telegram sends as one of them, is the channel's; see above).
- **Limits:** at most 10 actions per person in 10 minutes. When Telegram
  cannot say who may do what, nothing is done.
- **Once only:** each action is recorded in `moderation_actions` under the
  Telegram update that asked for it, before Telegram is called, so an
  update delivered twice is acted on once.
  A record is dropped only when Telegram refuses; when it does not answer,
  or a server's error comes back instead, it may have acted, so the record,
  and a deleted message's text, stay.

She is told to use the tool only when an admin or owner asks, never on her
own initiative, and to say an action was done only when the tool reports
it done; she then says so in one short line, like "Bajarildi: Bek @bek
15:04 gacha yoza olmaydi".

### Join requests

A group whose admins approve new members, or an invite link made to ask
for approval, sends her every request to join (`chat_join_request`
updates), where she is an admin with the right to invite users; nowhere
else does Telegram send them. She answers them only where the joins guard
is on (a `join_captcha` rule, "Maria, guruhga qo'shilmoqchi bo'lganlarni
tekshir"). Elsewhere the group's admins vet the requests by hand, and a
button any requester could press would let them approve themselves past
those admins, so she does nothing with them; the only exception is the
spam guard. She answers them in code, with no model:

- Where the spam guard is on, a requester the spam lists (CAS, lols.bot)
  know as a spammer is declined at once (`declineChatJoinRequest`), with
  the joins guard on or not.
- Where the joins guard is on, anyone else gets a private message, in the language of their Telegram
  app, with one button, "Men odamman ✅" ("Я человек ✅", "I'm human ✅"),
  sent to the private chat Telegram names with the request: she may write
  there for a few minutes though they never started her. Pressing it within
  10 minutes lets them in (`approveChatJoinRequest`), and the message says
  so; the person comes from Telegram's report of the press, never from the
  button, so nobody else can press it for them. Spam bots asking to join
  never press it.
- A request not pressed in time is declined. Waiting requests are kept in
  `join_requests` (migration 0037) and taken once, by the press or by the
  decline, so that a request is answered once. On a server the polling
  command declines late ones every minute; on Vercel, where nothing runs
  between requests, every update the webhook handles looks for them, at
  most once a minute per instance, and the daily cleanup declines any no
  update noticed. In a quiet stretch a late request may so wait longer than
  10 minutes; a press after its time declines it then.
- When the private message cannot be sent (the requester's privacy
  settings, or Telegram's minutes having passed), she leaves the request to
  the group's admins, as it was before her: declining people because she
  could not reach them would turn away real ones. She leaves them too in a
  muted group, and in a group where she lacks the right to invite users,
  asked of Telegram with each request. An admin who answers a request by
  hand first wins: her later approval or decline is refused by Telegram and
  dropped. A request still waiting when the joins guard is turned off goes
  back to the admins: its button then lets nobody in, and it is not
  declined.

Since the joins guard takes the requests from the admins who answer them,
those admins may hand them over or take them back themselves: the
`join_captcha` tool turns it on (`on`) or off (`off`) and says whether it
is on (`status`) for the bot's owners, the group's creator and its admins
who may invite users, asked of Telegram (`getChatMember`) at that moment;
an admin without that right, a member, or anyone when Telegram cannot say,
is refused. The asker's own words must be about the join check ("kapcha",
"captcha", "qo'shilish", "so'rov", "tekshir", "заявки", "вступление",
"проверка") and, to turn it off, ask for that ("o'chir", "kerak emas",
"yo'q qil", "выключи", "убери", "off"), as with moderation, so a line in
the chat cannot talk her into it. It touches the group's own joins guard
and nothing else. The owners still turn it, and the other guards, on and
off through `group_rules` (`guard` `joins`, `unguard` `joins`), which only
they may use.

Turn the joins guard on, turn on *Approve new members* in the group (or
make invite links with *Request admin approval*) and give her *Invite users
via link*; turning any of them off returns the requests to the admins.

### Stickers

She can answer with a sticker instead of words, from packs an owner
imported; never words and a sticker together, as the owners asked: words
that come with a sticker marker are sent without it. The model never picks
a sticker: it answers with `[sticker CATEGORY]`, one of funny, sarcasm, flirting, jealousy, sad,
comfort, shock, celebration, facepalm, angry, wholesome and cool, and the
code sends the enabled sticker of that category used longest ago. She is told
to use one in about a third of her playful answers and never with facts, a
tool's result, moderation, serious talk or someone in distress. A chat gets
at most one sticker in 30 seconds, and a sticker that cannot be sent costs
only itself, never the answer.

An owner imports a pack with `/import_stickers https://t.me/addstickers/name`
(in a private chat, or `/import_stickers@Bot …` in a group). Pack emoji are
often meaningless, so the model looks at each sticker once, a moving one by
its thumbnail, and gives it up to three categories, a short description and
whether it is fit to send: anything sexual, hateful, gory or cruel is kept
but never sent. One request labels what it can in about 40 seconds; sending
the command again goes on where it stopped, and the reply says how many are
left. The stickers are kept in `bot_stickers`, and when each chat last got
one in `sticker_sends`.

Now and then, about one time in four, she asks for `[gif CATEGORY]` instead.
With `GIPHY_API_KEY` set, the code searches GIPHY for that mood (rated PG at
most), picks one of the first ten at random and sends it; the GIF and the
stickers share the 30-second gap. When the search or the send fails, or
there is no key, a sticker of the same category goes instead. Asked for a
GIF of something, she writes `[gif TOPIC]`, the topic in a few English words
such as `[gif pubg]`, and GIPHY is searched for those words; a topic has no
sticker to fall back on, so when it fails nothing is sent. GIPHY's free
key allows 100 searches an hour, far more than she sends.

#### Her own sticker set

Owners fill a sticker set of her own from the chat, with the owner-only
`sticker_set` tool, offered when `BOT_OWNER_USERNAME` is set and refused in
code to anyone else. The set is
`https://t.me/addstickers/maria_by_<bot username>`, one per bot, so a test
bot sharing the database has its own; its first title is
`BOT_DISPLAY_NAME`.

- `add`: "Maria, shuni stiker qil" with a photo, or in reply to one (a
  static sticker of another pack works too), makes that picture a sticker;
  "Maria, mushuk stikerini chizib qo'sh" has it drawn first by
  `IMAGE_MODEL`, when drawing is on, on a plain white background that is
  then cut away from the edges in. The model gives 1 to 3 emoji for the
  sticker's mood (each checked in code to be one emoji, 20 at most). The
  picture is resized in Go to 512 pixels on its longer side and written as
  a PNG of at most 512 KB, with fewer colours when a photo's detail would
  make it larger, as Telegram requires of a static sticker. It is uploaded
  with `uploadStickerFile`; the first sticker makes the set
  (`createNewStickerSet`) as the asking owner's, and every later one is
  added (`addStickerToSet`) as that same owner, whoever asks, because
  Telegram lets only the set's owner add to it. That owner is kept in
  `own_sticker_sets`; should the row be lost, the next sticker finds the
  name taken and is added to the set as the asking owner. The new sticker
  is then sent to the chat in reply, with the set's link as the answer, and
  the model looks at it as at an imported pack's stickers, so that she
  sends it too; one not looked at in time is looked at with the next.
- `remove`: in reply to one of the set's stickers, takes it out
  (`deleteStickerFromSet`) and drops it from `bot_stickers`. A sticker of
  another pack is refused.
- `title`: renames the set (`setStickerSetTitle`, 1 to 64 characters), once
  it is made.
- `link`: the set's link, title and number of stickers.

Each owner adds at most 20 stickers a day, counted in `sticker_uploads`
from midnight in `GROUP_HISTORY_TIMEZONE`; a sticker that fails is not
counted. Telegram holds 120 stickers in a static set and paces additions,
and its refusal, with how long to wait, is passed on. The owner who makes
the set should have started a private chat with the bot first: Telegram
may refuse to make a set for a user the bot has never talked to.

### Boosts

When someone boosts a group where she is an admin, Telegram sends her a
`chat_boost` update, and she thanks them there, in code, without the model:
"Rahmat, Ali, guruhga boost berganingiz uchun! 🚀", in Russian or English for
an app in those languages, and without a name for a boost Telegram names no
one for. Each boost is thanked once: its ID is noted in `group_boosts` (the
group and the boost's ID, nothing about who gave it) before she speaks, as
Telegram sends a boost again when it changes, such as when it is renewed.
When it cannot be noted, or the bot is muted there, she says nothing. A boost
taken away gets no word, so she does not ask for `removed_chat_boost`.

### Polls

Anyone in a group can ask her for a poll ("Maria, so'rovnoma qil: plov yoki
manti?"). She posts an anonymous poll in that group, with 2 to 10 options
and one or several answers allowed, at most one per group every 10 minutes.
Polls are counted in `moderation_actions` as `poll`, with the poll's message
once sent. In a private chat she cannot make one.

She also closes a poll she made there (`create_poll` with action `stop`,
Telegram's `stopPoll`) when asked in words ("so'rovnomani yop", "ovoz
berishni to'xtat", "закрой опрос"): the one replied to, which must be her
own and still open, or else her latest open one. Only whoever asked for that
poll, the group's admins, as Telegram says when asked, and owners known by
`BOT_OWNER_IDS` may; she then gives the final counts, which Telegram also
shows under the closed poll. Closing is recorded as `poll_stop` with the
poll's message, so the same update closes once; it counts as no moderation.
A poll made before this version has no message kept and cannot be closed by
her.

### Dice

Asked to roll, throw or play ("Maria, zar tashla", "kubik ot", "бросай
кубик", "o'ynaymiz"), in a group or a private chat, she has Telegram roll
one of its animated dice with the `send_dice` tool: 🎲 a die, 🎯 darts, 🏀
basketball, ⚽ football, 🎳 bowling or 🎰 the slot machine. Telegram draws
the value, not the model nor the asker; the tool gives it back, with what
it means (a bullseye, a goal, a strike, the slot's three reels and whether
they won), and she comments in a line. One roll per message, and a chat
gets 3 rolls every 5 minutes, so a game cannot flood a group. Rolls are
counted in `moderation_actions` as `dice`, which, like polls, do not count
as anyone's moderation.

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

Asked to call or tag someone, she looks their @username up there. A member
without one is listed with a tag, `[Name](tg://user?id=123)`; written in her
answer, the telegram client sends it as their name linked to their ID (a
`text_link` entity), which Telegram shows as a mention and tells them of.

### Catching up

Each answer in a group shows her only an hour or two of a busy group's log.
Asked what was talked about or what someone missed ("nima gaplashdilar",
"что я пропустил"), she calls the `catch_up` tool, which reads the group's
log from `group_messages`: from the asker's own previous message, or the
hours they name, at most 24 and no older than `RETENTION_DAYS` keeps. Each
message is cut to 200 characters and the newest 32 KB are kept; she recaps
them in a few lines. Only the group the question is asked in is read, and in
a private chat the tool is not offered.

### Statistics

Asked who is the most active, the group's MVP or how much it wrote ("eng
faol kim", "guruh MVP si"), she calls the `group_stats` tool: for today
(since midnight in `GROUP_HISTORY_TIMEZONE`) or the last 7 days, the
messages in the group's log, how many members wrote, the ten who wrote the
most (the first is the MVP), the three busiest hours and whom the swear
guard caught the most. Her own messages are not counted, nor those written
while she was muted or older than `RETENTION_DAYS`. Members are named by
first name only, so that the list tags nobody. Only the group the question
is asked in is counted.

### Dashboard (Mini App)

With `WEBAPP_URL` set, the same numbers are a page: a Telegram Mini App
served from `public/stats/`, with the messages per hour of the day (in
`GROUP_HISTORY_TIMEZONE`), the busiest hour, the ten who wrote the most,
which of the group's guards are on (swearing, spam, links, pictures,
joins) and whom the swear guard caught the most, for today or the last 7
days. It follows the user's Telegram theme, light or dark, and speaks
Uzbek, or Russian to a Russian Telegram app.

It is for the bot's owners known by `BOT_OWNER_IDS` alone: it names who
writes and who swears most, which a group's members are not to see. To
anyone else `/stats` is an unknown command, and it is in no command menu
or `/help`.

- **In a group** an owner's `/stats` answers with a button that opens it. Telegram lets
  a Mini App button (`web_app`) be used in private chats only, so in a group
  the button is a link to the bot's main Mini App,
  `https://t.me/<bot>?startapp=g<group ID without the minus>`, which opens
  it inside Telegram with the group in `start_param`.
- **In a private chat** an owner's `/stats` answers with a Mini App
  button, and the menu button of each owner's private chat opens it too
  (`setChatMenuButton` for that chat, set at each start while `WEBAPP_URL`
  is set, when everyone else's is put back to the commands; without it the
  buttons are left as they are). Opened without a group, it lists the
  groups the bot is in, to pick one.

The page asks `/api/stats` for the numbers, sending the init data Telegram
gave it. The function believes nothing else:

- it checks the init data's `hash` as Telegram prescribes (HMAC-SHA256 with
  the key `HMAC-SHA256("WebAppData", bot token)`) and refuses data signed
  more than an hour ago, so the user's ID is Telegram's, never the page's;
- it shows nothing to anyone but the owners, whatever the link or the page
  names (`not_owner`);
- only the group asked about is counted, with the `group_stats` tool's
  queries.

Members are shown by the name the group log has for them, with their
@username; only the owners can see it.

### The game: Tarozi

With `WEBAPP_URL` set, `/game`, in a group or a private chat, posts the
bot's own game with `sendGame`, and so does the `send_game` tool when
someone asks to play a game ("o'yin o'ynaymiz", "скинь игру"; dice stay
with `send_dice`), once per message and twice per chat in 10 minutes,
counted in `moderation_actions` as `game`.

Tarozi is a bazaar scale. A price board gives four fruits a weight from 1
to 9 each, and changes every 6 weighings; each weighing puts fruits on the
two pans, one to four on each as the play goes on, and the player says
which pan goes down, or that they balance. A play lasts 40 seconds; a right
answer is 10 points and 2 more for each right one before it in a row (at
most 10 more), a wrong one takes 5 off. The page is `public/game/`, plain
HTML, and follows the device's light or dark mode.

Telegram keeps the scores: each game message has its own table, which it
shows under the message, and the page shows it too (`getGameHighScores`).
The score is not taken from the page:

- A press of Play reaches the bot as a callback query, answered with the
  page's address and a **session token** the bot signed (HMAC-SHA256, its
  key made from the bot token) naming the player, the game message and the
  time; it opens plays for an hour.
- Each play is **dealt by the server** from a fresh random seed (`start`),
  and comes with a run token that holds the seed and the start time.
- The page sends back only the **answers and when each was given**
  (`finish`); the server deals the same rounds from the seed and **counts
  the score itself**. It refuses a play with answers under 300 ms apart,
  after the 40 seconds, more than were dealt, or spread over more time than
  the server saw pass since the start, and run tokens older than 10
  minutes. Then it sets the score on the session's game message for the
  session's player (`setGameScore`; Telegram keeps each player's best).

A bot can still answer as fast as the limits allow, but not faster, and
cannot name its score, another player or another message.

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
  `/unmute`, `/ignore` and `/unignore` from those who may use
  them. A group's admins have no limit of their own: past it they are
  answered while the group's minute allows, so that an admin's "mute qil"
  is never lost to answers spent before it; and one whose message replies
  to another member's, as asking to moderate or ignore them does, is
  answered past the group's minute too.
  When the count cannot be read she stays silent and logs a warning.
- The per-user limit, `USER_RATE_LIMIT_PER_MINUTE`, is a flood guard kept in
  memory. In a private chat its first refusal is announced; in a group it
  never is.
- One group's messages are answered one at a time, in order.
- A message addressed to her in a group is stored and passed on as a single
  line: line breaks become spaces and square brackets become round ones. A
  private message keeps its lines, but its square brackets become round
  too, so nobody can type the bracketed note that marks the owner.
- The managing commands (`/mute`, `/unmute`, `/ignore`, `/unignore`)
  from those who may use them are not held back by the quotas, and
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
sees for the last 10 hours and which is deleted after `RETENTION_DAYS`.
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
| The last 10 hours of group messages as context | from the `group_messages` table | the same |
| Mute state | cached in memory | read from the database for each addressed group message |
| Group reply quotas, replies to bots, moderation, poll and dice limits | in the database | the same |
| Per-user flood guard | exact | per instance only, so much weaker; do not rely on it |
| Order of messages in a group | one at a time, in order | concurrent requests are not ordered |
| Retention cleanup (`RETENTION_DAYS`) | hourly | daily, by Vercel Cron at 03:00 UTC |
| Declining join requests not pressed in 10 minutes | every minute | by the next update the webhook handles, at most once a minute per instance, and the daily cleanup |
| Time per message | `REQUEST_TIMEOUT_SECONDS` | must fit in the function's 60 seconds |
| Dashboard and game pages and their APIs (`WEBAPP_URL`) | served by a Vercel deployment of this project, which may run beside the polling bot: they need only the bot token and the database | served by the same deployment |

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
5. **Let each group's old conversation expire**: each turn replays the
   group's last `MAX_HISTORY_MESSAGES` (20) stored messages whatever their
   age, and the stored ones are the old put-downs, Latin answers to Cyrillic
   and "Xo'p bo'ladi, xo'jayin" lines that the rewritten character must not
   copy. `RETENTION_DAYS` removes them; standing rules stay.

Run `set-webhook` again after this update: the bot now asks Telegram for
`chat_member` updates too, and Telegram keeps sending only the kinds named
when the webhook was last set. Until then the member list still fills from
messages, joins and leaves.

The forum topics, invite link limits and channel broadcasts need migration
0039 (the `channels` table and two columns of `broadcasts`) applied with
`make migrate` before deploying; the new version refuses to start on the old
schema. They ask Telegram for no new kind of update, so the webhook needs no
`set-webhook`. Give Maria *Manage topics* in forum groups where she should
act on topics (and *Delete messages* to delete one), and add her to a
channel as an admin with *Post messages* for broadcasts to reach it.

Her own sticker set needs migration 0042 (`own_sticker_sets` and
`sticker_uploads`) applied with `make migrate` before deploying; it changes
nothing that exists, and needs no `set-webhook` or new setting. The owner
who will make the set should write to the bot in private once before
asking for the first sticker.

### Deploying premium and gifts

1. **Apply migration 0038 before deploying**: `make migrate` with
   `DB_POSTGRES_URL` pointing at the production database. It adds
   `star_payments` and `premium_users` and changes nothing else; the new
   version refuses to start on the old schema.
2. **Set in Vercel** `PREMIUM_PRICE_STARS` to the price (for example `100`),
   and `PREMIUM_DAYS` and `PREMIUM_DAILY_LIMIT` to change their defaults of
   30 and 50. Without a price nothing is sold; the price can also be set
   later with `/set PREMIUM_PRICE_STARS 100`.
3. **Deploy.** The bot now also asks Telegram for `pre_checkout_query`
   updates, without which no payment goes through. Each cold start compares
   the webhook's update kinds with the bot's and sets the webhook again when
   they differ (the log says `webhook set again to receive every update kind
   the bot handles`); to do it at once, run
   `go run ./cmd/bot set-webhook https://YOUR-APP.vercel.app/api/webhook`.
4. **For gifts**, set `BOT_OWNER_IDS`, and have Stars on the bot's balance.

The edited messages, join requests and purge update needs:

1. **Apply migration 0037 before deploying** (`make migrate`, as above): it
   adds the `join_requests` table and lets `group_rules` hold the
   `join_captcha` kind, and changes nothing else. The new version refuses to
   start without it.
2. **Deploy.** The function, when it starts, sees that the webhook asks for
   fewer kinds of update than the bot handles and sets it again with
   `edited_message` and `chat_join_request`, keeping its address and
   secret (logged as "webhook set again to receive every update kind the
   bot handles"). Running `go run ./cmd/bot set-webhook https://YOUR-APP.vercel.app/api/webhook`
   does the same by hand.
3. **In each group that should check join requests**, an owner turns the
   joins guard on ("Maria, guruhga qo'shilmoqchi bo'lganlarni tekshir"),
   and the group gives her *Invite users via link* and turns on *Approve new
   members*. Until then she leaves every group's requests alone, but for
   declining known spammers where the spam guard is on. Purge needs *Delete
   messages*, which she has for deletions already.

The guard fixes (pictures judged once ever and by the model when
Sightengine cannot, CAS's key, the `join_captcha` tool) need:

1. **Apply migration 0040 before deploying** (`make migrate`, as above): it
   adds the `picture_verdicts` table and changes nothing else. The new
   version refuses to start without it.
2. **Set in Vercel** `CAS_API_KEY` to the Combot API key (see
   [Configuration](#configuration)) for the spam guard to ask CAS again;
   without it lols.bot alone answers, and the `401` warnings stop. The
   Sightengine keys may stay: Sightengine judges again when its month comes
   round, and the model meanwhile.
3. **Deploy.** No new kind of update is asked for.

### Deploying Telegram Business

1. **Apply migration 0041 before deploying** (`make migrate`, as above). It
   adds `business_connections`, `business_chats`, `business_replies` and
   `business_checklists` and changes nothing else; the new version refuses
   to start without it.
2. **Set `BOT_OWNER_IDS`** in Vercel if it is not set: only those owners'
   business accounts are used. `BUSINESS_DAILY_LIMIT` defaults to 30.
3. **Deploy.** The bot now also asks Telegram for `business_connection` and
   `business_message` updates. The function sets the webhook again at its
   first start when the kinds differ (logged as "webhook set again to
   receive every update kind the bot handles"); `go run ./cmd/bot
   set-webhook https://YOUR-APP.vercel.app/api/webhook` does it at once.
   Long polling asks for them on every request.
4. **In @BotFather**, open the bot's settings and turn on *Business Mode*;
   without it Telegram does not offer the bot as a chatbot.
5. **Each owner with Telegram Premium** connects her: Settings → Telegram
   Business → Chatbots, add the bot, choose the chats, and turn on replying
   to messages and managing stories. She writes to them with what she may
   do; `/business` shows it again, and `/business on` starts her answering
   customers, which is off at first.

The keyboard, effects, closing polls, sticker packs, boosts and map pins
update needs:

1. **Apply migration 0043 before deploying** (`make migrate`, as above): it
   adds the `group_boosts` table and changes nothing else. The new version
   refuses to start without it.
2. **Deploy.** The function sees that the webhook asks for fewer kinds of
   update than the bot handles and sets it again with `chat_boost`, as
   above. Each start also sets the admin rights she asks for and the menus.
3. Telegram sends boosts only where she is an admin; a group's sticker pack
   needs *Change group info*, and Telegram allows one only in large enough
   supergroups. Polls made before this version cannot be closed by her.

### Deploying the dashboard and the game

They need no migration and no new kind of update.

1. **Deploy**, then **set `WEBAPP_URL`** in Vercel to the deployment's
   production address, for example `https://maria.vercel.app` (no trailing
   path), and deploy again. Check the pages: `https://YOUR-APP.vercel.app/stats/`
   says it opens inside Telegram, and `/game/` asks for `/game`.
2. **The dashboard, in @BotFather:** `/mybots` → the bot → *Bot Settings* →
   *Configure Mini App* → *Enable Mini App*, and send
   `https://YOUR-APP.vercel.app/stats/` as its URL. This is the bot's main
   Mini App, which the groups' `t.me/<bot>?startapp=…` links open. The menu
   button of the owners' private chats is set by the bot itself at each
   start.
3. **The game, in @BotFather:** `/newgame` → the bot → accept the terms →
   a title (`Tarozi`), a short description, a 640×360 photo (and optionally
   a GIF) → the short name **`tarozi`**, exactly, which the bot sends the
   game by. No URL is asked for: the bot gives `https://YOUR-APP.vercel.app/game/`
   with each press of Play.
4. As an owner, send `/stats` and `/game` in a group and in a private chat.

Until step 2 the group links open nothing; until step 3 `/game` answers
that something went wrong, and `send_game` tells the model the short name
is unknown.

### Daily cleanup

`vercel.json` schedules a Vercel Cron job that calls `/api/cleanup` every
day at 03:00 UTC. It declines the join requests whose button was not
pressed in time and that no update noticed (see
[Join requests](#join-requests)), and deletes chat history older than `RETENTION_DAYS`
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
by an hourly job, or daily by Vercel Cron. The counts behind the limits (`image_generations`, `bot_replies`,
`group_replies`, `private_messages`) hold IDs and times, no message text, and
are deleted with the history. Standing rules are deleted when they expire. The moderation audit
(`moderation_actions`) is never deleted automatically; delete
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
- [x] The dashboard's API takes the user only from Telegram's signed init
      data (checked with the bot token, at most an hour old) and shows
      nothing to anyone but the owners known by `BOT_OWNER_IDS`. The game's API
      sets a score only for the player and message in a session the bot
      signed, and counts the score itself from the answers.
- [x] All SQL is parameterized. The model cannot produce SQL that is run.
- [x] Every query on a conversation is keyed by the chat and user IDs taken
      from the update, never by an ID in message text.
- [x] Only messages from human senders are accepted: in private chats, and in
      groups when they address the bot. The bot's identity for that check
      comes from Telegram (`getMe`), not from configuration.
- [x] Private and group conversations are stored under separate keys and are
      never loaded together; one group never sees another's history.
- [x] Group messages are stored in `group_messages` and deleted after
      `RETENTION_DAYS`; the model is given only the last 10 hours of them.
      They are never written to the logs, and not stored at all while a
      group is muted.
- [x] Quoted messages and recent group messages are marked as untrusted and
      shortened. Square brackets are replaced in them, in author names and in
      group messages, and group messages and names are folded to one line,
      so that no text can imitate a marker or another speaker in the shared
      history.
- [x] `/mute`, `/unmute`, `/ignore` and `/unignore` are
      limited to that group's administrators, checked with Telegram, the
      bot's own administrators and its owners.
- [x] Moderation is checked in code, never by the model: the target is the
      message the person replied to, from the Telegram update; the person
      asking must be an owner or an admin holding the right, and the bot
      must hold it too, both asked of Telegram at that moment; admins,
      owners and Telegram's service accounts are never targets, nor is the
      bot, except to delete or pin its own message; at most 10 actions per
      person in 10 minutes; nothing is done unless the person's own message
      asks for it in words, read in code; an update delivered twice acts
      once. When Telegram cannot be asked, nothing is
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
- [x] Payments are judged in code, never by the model: Telegram's check
      before a payment and the payment itself are compared with the plan on
      sale and the payer's ID from Telegram, and a payment counts once by its
      charge ID. Only owners see or refund payments, in a private chat.
- [x] Gifts spend the bot's Stars only for owners known by ID, for the
      member replied to or tagged, taken from the update, when the owner's
      own words ask for a gift, and after the balance is checked.
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
- Message text is sent to the model provider, Google. Tell your users, and
  check the provider's data policy.
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
| The bot ignores `/mute` in a group                        | Commands in groups need the bot's username: `/mute@YourBot`.                                                                                                                                                                                                                                  |
| "Only this group's admins can do that"                    | `/mute`, `/unmute`, `/ignore` and `/unignore` in a group are for its admins. Anonymous admins are not recognised; turn "Remain anonymous" off or add your ID to `ADMIN_USER_IDS`.                                                                                                                           |
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
| A payment fails after Pay is tapped                       | Telegram got no answer to its check before the payment: the webhook was set before the bot asked for `pre_checkout_query`. Redeploy, or run `set-webhook` again. `payment refused before it was made` in the log is the bot saying no, with the reason. |
| "That's today's limit", then silence, in a private chat   | The private daily limit: the log shows `private daily limit reached`. Raise `PRIVATE_DAILY_LIMIT`, or `0` to turn it off; owners are never limited. `private message count failed; answering anyway` means the database could not be asked. |
| Customers of a business account get no answer             | `/business` in the owner's private chat says what is missing: no connection, no right to reply, or auto-reply off. The owner must be in `BOT_OWNER_IDS` (the log shows `a business account that is none of the owners' connected the bot`); the chat must be one the owner gave her; she leaves a chat to the owner for 15 minutes after they write there; past `BUSINESS_DAILY_LIMIT` she is silent (`business daily limit reached`). No `business_message` at all: the webhook predates the update kinds; redeploy or run `set-webhook`. |
| "I'm handling a lot of messages right now"                | Worker queues are full. Raise `MAX_CONCURRENT_REQUESTS`, and `pool_max_conns` to match.                                                                                                                                                                                                       |
| The bot forgot the conversation                           | History is limited to `MAX_HISTORY_MESSAGES`, and idle conversations are deleted after `RETENTION_DAYS`.                                                                                                                                                                                      |
| Service will not start under systemd                      | `journalctl -u telegram-ai-agent -n 50`. Check the environment file's owner and mode, and that it has no quotes around values.                                                                                                                                                                |

More detail is available with `LOG_LEVEL=debug`.
