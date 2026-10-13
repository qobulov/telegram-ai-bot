# Group Mentions and Replies Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the bot answer in groups when mentioned, replied to, or addressed by name, replying to the original message, with group history stored separately from private history.

**Architecture:** Addressing rules live in a new pure file in `service` and run before an update is queued. Conversations become keyed by `(chat_id, user_id)`, carried through `agent` and `storage` as a `storage.Scope`. The Telegram client learns entities, `getMe` and `reply_parameters`.

**Tech Stack:** Go 1.27, standard library, `pgx/v5`, PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-10-13-group-mentions-design.md`

**Note on format:** as with the first plan, this fixes files, signatures, behaviour and test cases without duplicating function bodies; it is executed by its author.

## Global Constraints

- Work continues on `feat/telegram-ai-agent`. No new dependencies.
- Private-chat behaviour must not change; every existing test keeps passing (adapted only where a signature changed).
- User ID and chat ID always come from the update, never from message text.
- Never log message text, quoted text, tokens or the database URL.
- Each task ends with `gofmt -l .` empty, `go vet ./...` clean, `go test -race -p 1 ./...` green (with `TEST_DATABASE_URL` set), and a commit.

## Review Focus

1. A mention of a different account that shares a prefix with the bot (`@MyAssistantBot2`, `@MyAssistant`) → not addressed. Test in Task 4.
2. An entity whose offset or length runs past the end of the text → ignored, no panic. Test in Task 2.
3. A reply quoting a long message that contains instructions → the quote is marked untrusted and cut to 1,000 characters. Test in Task 4.
4. Negative chat IDs (every group and supergroup) → worker selection and the group limiter work, no panic. Test in Task 4.
5. Private conversations saved before migration 0002 → still loaded afterwards under the private scope. Test in Task 3.

---

### Task 1: Configuration

**Files:** `internal/config/config.go`, `config_test.go`, `.env.example`

**Produces** (new `Config` fields):

```go
MentionTrigger, ReplyTrigger, DisplayNameTrigger bool
BotDisplayName    string
GroupSystemPrompt string
MaxGroupReplyChars int
GroupRateLimitPerMinute int
```

- [ ] Tests first: defaults (`true`, `true`, `false`, `""`, built-in group prompt containing "Telegram group", 4000, 10); booleans accept `true/false/1/0` in any case and reject `yes`; `ENABLE_DISPLAY_NAME_TRIGGER=true` without `BOT_DISPLAY_NAME` is an error naming `BOT_DISPLAY_NAME`; `GROUP_CONTEXT_MODE=shared` and `=bogus` rejected, `per_user` and empty accepted; `MAX_GROUP_REPLY_LENGTH` 0 and 4097 rejected, 4096 accepted; `GROUP_RATE_LIMIT_PER_MINUTE=0` rejected.
- [ ] Watch them fail, implement, verify, commit.

### Task 2: Telegram client

**Files:** `internal/telegram/types.go`, `client.go`, new `entities.go`, `entities_test.go`, `client_test.go`; call sites in `internal/service` and `cmd/bot` adapted to compile.

**Produces:**

```go
type User struct { ID int64; IsBot bool; Username string }
type MessageEntity struct { Type string; Offset, Length int; User *User }
type Message struct {
    MessageID int64; From *User; Chat Chat
    Text, Caption string
    Entities, CaptionEntities []MessageEntity
    ReplyToMessage *Message
}
const ChatTypeGroup, ChatTypeSupergroup = "group", "supergroup"
const EntityMention, EntityTextMention, EntityBotCommand = "mention", "text_mention", "bot_command"

func (m *Message) Content() (text string, entities []MessageEntity) // text, else caption
func EntityText(text string, e MessageEntity) (string, bool)        // false when out of range
func RemoveEntities(text string, remove []MessageEntity) string     // cuts the given ranges

type Outgoing struct { ChatID int64; Text string; ReplyToMessageID int64 }
func (c *Client) GetMe(ctx context.Context) (User, error)
func (c *Client) SendMessage(ctx context.Context, msg Outgoing) (messageID int64, err error)
```

`SendMessage` returns the ID of the first chunk; `reply_parameters` (`message_id`, `allow_sending_without_reply: true`) is sent only on the first chunk and only when `ReplyToMessageID != 0`.

- [ ] Tests first. Entities: `"@bot hi"`; `"😀😀 привет @bot hi"` (offset counted in UTF-16 units); entity at the end of text; offset past the end, length past the end, negative values → `false` (Review Focus 2); `RemoveEntities` with one and two ranges, with emoji before them, and with an out-of-range entity left alone; `Content` prefers text, falls back to caption and its entities. Client: `GetMe` decodes ID and username; `getUpdates` decodes `entities`, `caption_entities`, `reply_to_message.from`; `SendMessage` with `ReplyToMessageID` posts `reply_parameters.message_id` and `allow_sending_without_reply`, without it posts no `reply_parameters`; a two-chunk message replies only on the first request; the returned ID is the first chunk's `result.message_id`.
- [ ] Watch them fail, implement, adapt `service.Sender` and `cmd/bot` so everything compiles with unchanged behaviour, verify, commit.

### Task 3: Storage scope and agent request

**Files:** new `internal/storage/migrations/0002_group_scope.sql`; `internal/storage/conversations.go`, `store_test.go`; `internal/agent/agent.go`, `agent_test.go`; call sites in `service` and `cmd/bot` adapted to compile.

**Produces:**

```go
// storage
type Scope struct { ChatID, UserID int64 }
type Turn struct {
    Scope            Scope
    UpdateID         int64
    MessageID        int64 // incoming Telegram message
    ReplyToMessageID int64 // message it replied to, 0 if none
    Messages         []ai.Message
}
func (s *Store) History(ctx context.Context, scope Scope, limit int) ([]ai.Message, error)
func (s *Store) SaveTurn(ctx context.Context, turn Turn) (answerID int64, err error)
func (s *Store) SetSentMessageID(ctx context.Context, answerID, telegramMessageID int64) error
func (s *Store) Reset(ctx context.Context, scope Scope) error

// agent
type Request struct {
    Scope            storage.Scope
    UpdateID         int64
    MessageID        int64
    ReplyToMessageID int64
    Text             string
    Group            bool
}
type Answer struct { Text string; ID int64 }
func (a *Agent) Reply(ctx context.Context, req Request) (Answer, error)
// Options gains GroupSystemPrompt string
```

Zero IDs are stored as NULL. The last message of a turn gets `reply_to_message_id = turn.MessageID`.

- [ ] Storage tests first: all existing tests ported to scopes; same user, private scope vs group scope → separate histories; two users in one group → separate; `Reset` of the group scope leaves the private one; message and reply IDs stored on the user row, and the answer row's `reply_to_message_id` equals the incoming message; `SetSentMessageID` sets the answer's `telegram_message_id`; duplicate `update_id` in a different scope → `ErrDuplicateUpdate`; **migration test:** apply only 0001, insert a private conversation with SQL, apply 0002, then `History(Scope{ChatID: u, UserID: u})` returns it (Review Focus 5).
- [ ] Agent tests first: existing tests ported to `Request`; `Group: true` sends `GroupSystemPrompt`, `false` sends `SystemPrompt`; the turn passed to the store carries scope, update ID, message ID and reply ID; `Answer.ID` is the store's returned row ID; tools are executed with `Scope.UserID` as caller.
- [ ] Watch them fail, implement, adapt call sites, verify, commit.

To let the migration test stop after 0001, `Migrate` is split so an unexported `migrateTo(ctx, maxVersion)` does the work.

### Task 4: Service — addressing and group behaviour

**Files:** new `internal/service/trigger.go`, `trigger_test.go`; `service.go`, `commands.go`, `texts.go`, `service_test.go`.

**Produces:**

```go
type Identity struct { ID int64; Username string }

// Options gains:
Bot                Identity
MentionTrigger, ReplyTrigger, DisplayNameTrigger bool
DisplayName        string
GroupRatePerMinute int
MaxGroupReplyChars int
Recorder           Recorder // SetSentMessageID(ctx, answerID, telegramMessageID) error

type Replier interface { Reply(ctx context.Context, req agent.Request) (agent.Answer, error) }
type Resetter interface { Reset(ctx context.Context, scope storage.Scope) error }
type Sender interface { SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error) }

// trigger.go
type addressed struct { text string; isCommand bool }
func (s *Service) address(msg *telegram.Message) (addressed, bool)
func withQuote(text string, replied *telegram.Message, botID int64) string
```

`Handle` accepts private, group and supergroup messages from human senders; for groups it calls `address` and drops the update when not addressed, before queueing. Workers stay keyed by user ID.

- [ ] Trigger tests first (table-driven): `@MyBot hi` → `hi`; `@mybot` case-insensitive; `@MyBot2`, `@MyBo` and another user's `text_mention` → not addressed (Review Focus 1); `text_mention` with the bot's ID; caption mention; mention in the middle (`hey @MyBot what is Go`) → `hey what is Go`; reply to the bot; reply to another user without mention → not addressed; mention trigger disabled; reply trigger disabled; display name `Mira, explain` and `mira: explain` when enabled; `Miranda, hi`, `I think Mira is wrong`, `Mira explain` → not addressed; display name disabled; `/reset@MyBot` addressed as command; `/reset` and `/reset@OtherBot` → not addressed; plain chatter → not addressed. `withQuote`: other member's message gets the untrusted header; the bot's own message gets the "your earlier message" header; a 5,000-character quote is cut to 1,000 with `…` (Review Focus 3); no replied message or empty text → text unchanged.
- [ ] Behaviour tests first: group mention → `Replier` receives `Scope{ChatID: group, UserID: sender}`, `Group: true`, message ID and reply ID, and the stripped text; the reply is sent to the group chat with `ReplyToMessageID` = original message; private reply has `ReplyToMessageID` 0; group answer longer than the limit is cut with `…`; sent message ID is passed to `Recorder` with the answer ID; `Recorder` failure does not produce a second message; unaddressed group message → nothing sent, replier not called, limiter not charged; bot sender and `channel` chat type ignored; `/reset@MyBot` in a group resets the group scope only; addressed message with no text → "text only" reply; bare mention → hint; group rate limit: with user limit high and group limit 2, the third addressed message from different users gets one rate-limit reply and later ones are silent; negative chat ID and user ID map to a worker without panic (Review Focus 4); replier error and deadline → generic texts sent as replies to the message; duplicate → nothing sent.
- [ ] Watch them fail, implement, verify, commit.

### Task 5: Wiring, end-to-end test and documentation

**Files:** `cmd/bot/main.go`, `main_test.go`, `README.md`.

- [ ] End-to-end test first (extends the fake Telegram server with `getMe` and a group update that mentions the bot and replies to another member's message): the `sendMessage` request carries the group `chat_id` and `reply_parameters.message_id`; the model request contains the group system prompt and the marked quote; history is stored under the group scope and the private scope is empty; the answer row has the sent message ID.
- [ ] Wire `getMe` at startup (failure is a startup error), the new options, and `Recorder`.
- [ ] README: a "Groups" section (how to add the bot, the three triggers, example interactions, what is and is not sent to the model), BotFather privacy mode, the new settings in the configuration table, the migration step for existing deployments, troubleshooting rows (bot silent in a group; display name not working; replies to deleted messages; anonymous admins).
- [ ] Final verification with and without `TEST_DATABASE_URL`; `go build ./...`; smoke-run `migrate` on a database at version 1.
- [ ] Commit.
