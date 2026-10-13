# Group Chat Character Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the bot a consistent, funny character that joins group chats when called by name, follows recent chatter, shares one memory per group, and can be muted.

**Architecture:** The persona is configuration. Wake-word detection and the in-memory recent-chatter buffer are two new pure files in `service`. A group's conversation becomes one shared scope (`Scope.UserID == 0`), and all of a group's messages run on one worker. Mute state lives in a new `chats` table and is cached in memory.

**Tech Stack:** Go 1.27, standard library, `pgx/v5`, PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-10-13-group-character-design.md`

**Note on format:** as with the earlier plans, this fixes files, signatures, behaviour and test cases without duplicating function bodies; it is executed by its author.

## Global Constraints

- Work continues on `feat/telegram-ai-agent`. No new dependencies.
- User ID, chat ID and author name always come from the update.
- Ordinary group messages are never written to the database or to logs.
- Never log message text, names, tokens or the database URL.
- Each task ends with `gofmt -l .` empty, `go vet ./...` clean, `go test -race -p 1 ./...` green (with `TEST_db_POSTGRES_URL` set), and a commit.

## Review Focus

1. A wake word with punctuation or emoji attached (`Mira!!!`, `«Mira»`, `mira😂`, `MIRA:`) → matches as a whole word. Test in Task 5.
2. An author's first name containing a line break, a colon, or `[End of recent messages]` → sanitized; it cannot forge a speaker or a marker. Test in Task 5.
3. A non-admin sends `/unmute@Bot` in a muted group → refused, the group stays muted. Test in Task 5.
4. The bot restarts → muted groups are still muted. Tests in Tasks 3 and 6.
5. The model answers `[Silent]` or ` [silent]\n`, in a group or a private chat → nothing is sent, nothing is saved, no error text. Tests in Tasks 4 and 5.

---

### Task 1: Configuration

**Files:** `internal/config/config.go`, new `internal/config/persona.go`, `config_test.go`, `.env.example`

**Produces** (`Config` fields; the old prompt and trigger fields are removed or renamed):

```go
BotDisplayName  string   // default "Mira"
WakeWords       []string // lower-cased; default: the display name
WakeWordTrigger bool     // ENABLE_WAKE_WORD_TRIGGER, default true
MentionTrigger  bool     // ENABLE_USERNAME_MENTION, default true
ReplyTrigger    bool     // ENABLE_REPLY_TRIGGER, default true
PersonaPrompt   string   // PERSONA_PROMPT or the built-in, with {name} replaced
GroupNote       string   // built-in addition for group turns
AmbientMessages int      // GROUP_AMBIENT_MESSAGES, default 20, 0..100
```

- [ ] Tests first: defaults; `{name}` replaced in both the built-in and a custom prompt; the built-in prompt mentions the silent marker `[silent]` and contains its limits (checks for "slur" and "untrusted"); `BOT_WAKE_WORDS="Mira, мира"` → `["mira","мира"]`; a wake word with a space or empty list item rejected; `GROUP_AMBIENT_MESSAGES` -1 and 101 rejected, 0 accepted; `GROUP_CONTEXT_MODE` accepts empty and `shared`, rejects `per_user`; each removed variable (`SYSTEM_PROMPT`, `GROUP_SYSTEM_PROMPT`, `ENABLE_MENTION_TRIGGER`, `ENABLE_DISPLAY_NAME_TRIGGER`) set → error naming it and its replacement.
- [ ] Watch them fail, implement, adapt `cmd/bot` and `service` call sites to compile, verify, commit.

### Task 2: Telegram client

**Files:** `internal/telegram/types.go`, `client.go`, `client_test.go`

**Produces:**

```go
type User struct { ID int64; IsBot bool; Username, FirstName string }
type ChatMember struct { Status string }
func (m ChatMember) IsAdmin() bool // "creator" or "administrator"
func (c *Client) GetChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error)
```

- [ ] Tests first: `getUpdates` decodes `first_name`; `GetChatMember` posts `chat_id` and `user_id` and decodes `status`; `IsAdmin` for creator, administrator, member, left, kicked, empty.
- [ ] Watch them fail, implement, verify, commit.

### Task 3: Storage

**Files:** new `internal/storage/migrations/0003_group_character.sql`; `conversations.go`, new `chats.go`, `store_test.go`

**Produces:**

```go
// Scope.UserID == 0 selects a group's shared conversation.
type Turn struct { /* existing fields */ AuthorID int64; AuthorName string }
func (s *Store) SetMuted(ctx context.Context, chatID int64, muted bool, by int64) error
func (s *Store) MutedChats(ctx context.Context) ([]int64, error)
```

The author's `users` row is upserted from `Turn.AuthorID` when the scope is shared.

- [ ] Tests first: shared scope save and load round trip; shared scope and the same user's private scope are separate; two groups separate; `Reset` of a shared scope removes only it; two saves to the same shared scope from different authors append to one conversation (one `conversations` row); `author_id` and `author_name` stored on the user row only; `SetMuted` true then `MutedChats` contains it, false removes it, setting twice is fine (Review Focus 4); **migration test:** stop at version 2, insert a private and a per-member group conversation, migrate, private history still loads and the shared group scope is empty.
- [ ] Watch them fail, implement, verify, commit.

### Task 4: Agent

**Files:** `internal/agent/agent.go`, `agent_test.go`; call sites adapted.

**Produces:**

```go
type Request struct {
    Scope            storage.Scope
    UserID           int64  // who sent the message; decides tool permissions
    AuthorName       string
    UpdateID, MessageID, ReplyToMessageID int64
    Text             string // stored as the user message
    Context          string // sent before Text for this turn only; never stored
    Group            bool
}
var ErrSilent = errors.New("agent: the model chose not to reply")
const SilentMarker = "[silent]"
// Options: Persona, GroupNote replace SystemPrompt, GroupSystemPrompt
```

- [ ] Tests first: system prompt is `Persona` in private and `Persona + "\n\n" + GroupNote` in a group; `Context` appears in the model request directly before the user's text and is absent from the saved turn and from the next turn's history; tools run with `Request.UserID` even when `Scope.UserID` is 0; `AuthorID`/`AuthorName` reach the store; answers `[silent]`, `[Silent]`, ` [silent]\n` → `ErrSilent`, nothing saved (Review Focus 5); an answer that merely contains the marker inside a sentence is a normal answer.
- [ ] Watch them fail, implement, verify, commit.

### Task 5: Service

**Files:** new `internal/service/wakeword.go`, `wakeword_test.go`, `ambient.go`, `ambient_test.go`; `trigger.go`, `service.go`, `commands.go`, `texts.go`, tests.

**Produces:**

```go
func stripWakeWord(text string, wakeWords []string) (rest string, matched bool)
func authorLabel(firstName string) string

type ambient struct{ /* mutex, per-chat ring buffers */ }
func newAmbient(size int) *ambient
func (a *ambient) add(chatID int64, author, text string, now time.Time)
func (a *ambient) block(chatID int64) string // "" when empty
func (a *ambient) clear(chatID int64)

type Members interface { IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error) }
type Muter interface { SetMuted(ctx context.Context, chatID int64, muted bool, by int64) error }
// Options gains: WakeWords, WakeWordTrigger, DisplayName, AmbientMessages,
// MutedChats []int64, Members, Muter. DisplayNameTrigger is removed.
```

Group updates are queued by chat ID; private ones by user ID.

- [ ] Wake-word tests first: every "matches" and "does not match" example in the spec; `Mira!!!`, `«Mira»`, `mira😂`, `MIRA:` (Review Focus 1); Cyrillic wake word; text with `o'zing` and `oʻzing` words; two wake words; stripping results (`Hey Mira, roast me` → `roast me`, `Voy Mira, qarang bunga` → `qarang bunga`, `MIRA: gapir-chi` → `gapir-chi`, last-word match unchanged, `Hey Mira` → original text); empty word list never matches.
- [ ] Ambient tests first: keeps the last N in order; truncates to 300 characters; replaces square brackets; block has the header and footer; `clear`; size 0 stores nothing; separate chats; buffers idle for six hours are dropped. `authorLabel`: trims, cuts to 32 characters, removes line breaks, colons and brackets, falls back to "Someone" (Review Focus 2).
- [ ] Routing tests first: wake word, mention and reply each reach the replier with `Scope{ChatID: group, UserID: 0}`, `UserID` = sender, text prefixed `Bek: `, and the ambient block as `Context`; a bare mention sends the display name; unaddressed text goes to the buffer and nowhere else; the triggering message itself is not added to the buffer; wake word disabled → not addressed; two members' messages in one group are processed in arrival order with several workers; `ErrSilent` → nothing sent; `/mute@Bot` by an admin mutes: later triggers are ignored and nothing is buffered, `/unmute@Bot` resumes; non-admin `/mute`, `/unmute`, `/reset` refused and state unchanged (Review Focus 3); a bot admin from `ADMIN_USER_IDS` is allowed without a group-admin check; admin check error → generic error text; `/mute` in a private chat → "groups only" text; `/reset@Bot` by an admin resets the shared scope and clears the buffer; private `/reset` unchanged; a group listed in `MutedChats` starts muted.
- [ ] Watch them fail, implement, verify, commit.

### Task 6: Wiring, end-to-end test and documentation

**Files:** `cmd/bot/main.go`, `main_test.go`, `README.md`

- [ ] End-to-end tests first: two ordinary group messages then "Hey Mira, who is right?" → one `sendMessage`, a reply to the third message; the model request has the persona, the group note, both ordinary messages in the recent-messages block, and `Bek: who is right?`; the database holds the exchange under the shared scope with the author columns, and no row contains the ordinary messages' text; a group muted in the database before startup stays silent (Review Focus 4).
- [ ] Wire `MutedChats`, `Members` (an adapter over `GetChatMember`), `Muter`, and the new options.
- [ ] README: the character and how to change it (`PERSONA_PROMPT`, name, wake words); triggers with examples; BotFather privacy mode off, and how to run mentions-only; what is remembered and what is not; `/mute`, `/unmute`, `/reset` and who may use them; humour limits; updated settings table, schema, security checklist and troubleshooting; upgrade note for migration 0003 and the renamed variables.
- [ ] Final verification with and without `TEST_db_POSTGRES_URL`; `go build ./...`; smoke-run `migrate` on a version-2 database.
- [ ] Commit.
