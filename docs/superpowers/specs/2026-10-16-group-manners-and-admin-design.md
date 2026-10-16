# Group manners and admin powers

Date: 2026-10-16. Status: approved by the owner ("muammolarni fix qil"); the
calls marked *my call* were made without asking and can be revisited.

## Why

An audit of the production database (4 groups, 2008 group messages, 517 of
them Maria's, 10-14 to 10-16) found that Maria is poor company in groups and
cannot use the admin rights groups gave her:

- She floods: 40-44% of all lines in the busiest group in some hours, 16
  messages in one minute, 22 answers to "0"/"1" baits, about 85 word answers
  to stickers, GIFs and laughter. Every reply to her is a model call
  (`trigger.go` ReplyTrigger), and the rate limits are in-memory token
  buckets that do nothing on Vercel, where each request may run in its own
  instance.
- Her roasts turn into put-downs (about 100 of 106 replies in one stretch):
  intelligence, family, mental illness, suicide, lectures on manners, insults
  to newcomers. Prompt rules alone did not stop it.
- Owners' standing orders ("don't answer him", "no finance talk", "don't call
  me master") are forgotten within minutes: they live only in the day log,
  which `/mute` and `/reset` erase.
- She claims actions and facts she cannot have ("sent to all groups",
  "joined", "I saw the photo"), and the time tool answers in UTC.
- She answers Cyrillic and Russian in Latin Uzbek (0 of 517 replies in
  Cyrillic), writes 7-10 times longer lines than members, ends most replies
  with the person's name, and leaked `[silent]` into the chat 4 times.
- She knows only her membership status, has no Telegram moderation methods
  at all, and owners are identified by a username that could be recycled.

## Goals

1. Maria talks like a good group member: short, in the speaker's language
   and script, playful rather than cruel, quiet when nothing needs saying,
   and visibly present through emoji reactions instead of words.
2. Limits that matter are enforced in code and work on Vercel.
3. Owners' standing orders persist and are obeyed.
4. Maria uses her admin rights (delete, mute/unmute, ban/unban, pin/unpin)
   when a group admin holding the matching right, or a bot owner, asks; and
   anyone may ask her for a poll.
5. Owners can be identified by numeric Telegram ID.

## Non-goals (later projects)

Automatic moderation, promoting members, join requests, automatic welcome
messages, memory of people across days, sending stickers, edited messages,
tagging members without a username, inline buttons, provider logging,
public-API integrations (the next project).

## Anti-goals

- The model is never the authority for any admin action. A tool argument,
  a quoted or logged message, a voice transcript or another bot can never
  choose the target or grant a permission.
- No action narrated as done unless a tool returned it done.
- Maria must not look dead: silence is paired with reactions.

## Constraints

- Go, standard library plus pgx; data access stays in `internal/storage` on
  pgx like the 12 existing migrations (*my call*: the global CLAUDE.md asks
  for the Ucode SDK, which this repo has never used).
- Runs on Vercel (webhook, `HandleNow`, 60 s `maxDuration`, no memory
  between requests) and by long polling. Every limit and pending state
  lives in Postgres.
- No new `allowed_updates` are needed, so `set-webhook` need not be re-run.
  New migrations must be applied to production before deploying, because
  `newWebhook` refuses to start on an outdated schema. The local `.env`
  points at the production database: never run `migrate` without the
  owner's go-ahead; tests use `TEST_DB_POSTGRES_URL` on a local database.
- Follow the repo's style: small packages, interfaces at the consumer,
  doc comments that explain why, table-driven tests with fakes.

## Design

### 1. Who addressed her, and the reply gate (service)

`address` reports how a group message addressed the bot: `command`,
`mention`, `wakeword` or `reply`. It is kept on `incoming`.

`process` gains these steps, in order, after `silenced` and the bot check:

1. **Ignored sender.** If a standing rule says to ignore this member in this
   chat (section 3) and the sender is not an owner, stop. Nothing is sent.
2. **Applause.** A human non-owner whose only trigger is `reply`, and whose
   message is applause, gets an emoji reaction instead of an answer, and the
   message is logged to the day log. Applause is: a sticker or GIF without
   caption (photos, voice and video are content); text made only of emoji, punctuation, digits and
   laughter (`xa`, `ha`, `ах`, `ха`, `хх`, `lol`, `)))` and repeats); or at
   most 2 visible characters. A mention or wake word is never applause.
   Reaction choice is code-only: laughter gets 🤣, anything else one of
   👍 🔥 😁 👌 picked by `message_id % 4`.
3. **Quota (Postgres).** For group turns of human non-owners, a single
   `ReserveGroupReply(chatID, userID, now, perUser, perChat)` call, with a
   per-chat advisory lock, counts the chat's answers in the last minute
   (limit `GROUP_RATE_LIMIT_PER_MINUTE`, default lowered from 10 to 6) and
   this member's answers in the last 10 minutes (new
   `MEMBER_REPLIES_PER_10_MIN`, default 4), and records the answer if both
   allow. Over either limit she stays silent: no notice in groups. Mute
   commands and owners are exempt. A failed count stays silent and logs a
   warning, like `mayAnswerBot`. The in-memory group limiter and
   `textGroupRateLimited` are removed; the in-memory per-user limiter stays
   for private chats and as a flood guard.
4. Then describe, respond and deliver as today.

Rows older than a day are deleted by `Store.Expire`.

### 2. Reactions, typing, silence

- `telegram.Client.SetMessageReaction(ctx, chatID, messageID, emoji)` sends
  one emoji reaction; `SendChatAction(ctx, chatID, action)`.
- The model may answer with exactly `[react X]` to react instead of writing.
  `agent.finish` parses it; `X` must be one of Telegram's 73 allowed reaction
  emoji (a fixed list in code), otherwise the answer is treated as silent.
  The turn is saved with the assistant content `<reacted X>` so history stays
  coherent, and the service sets the reaction on the message it answers. The
  day log records `<You reacted X>`.
- `[silent]` anywhere in the answer, in any case and bracket style
  (`[silent]`, `(silent)`), makes the whole answer silent.
- While a turn runs, `typing` is sent at the start and refreshed every 4 s by
  a goroutine bound to the turn's context, stopped before the reply is
  delivered.
- Reaction or chat-action failures are logged at debug level and ignored.

### 3. Standing rules

Migration adds `group_rules`:

| column | notes |
|---|---|
| id bigserial | |
| chat_id bigint | the group; 0 for rules that apply everywhere |
| kind text | `instruction`, `ignore`, `address` |
| user_id bigint null | the ignored member, or the owner an address is for |
| user_name text | shown in lists and the header |
| text text | the instruction or the form of address, at most 300 chars |
| set_by bigint | |
| created_at, expires_at (null = never) | |

- Tool `group_rules`, owners only: `add_instruction` (text, optional
  `hours`, default 168, max 720), `ignore` (the member whose message the
  owner replied to; optional `hours`, default 24, max 720), `unignore` (same
  target), `set_address` (text: how Maria addresses the calling owner,
  everywhere), `list`, `remove` (id). Rules apply to the chat the owner is
  in; in a private chat only `set_address`, `list` and `remove` make sense.
  At most 20 active instructions per chat.
- `/ignore@bot` and `/unignore@bot`, sent as a reply to a member's message,
  for group admins and owners (`refuseUnlessManager`). Owners, group admins
  and the bot itself cannot be ignored.
- Every group turn loads the chat's active rules once. Instructions and the
  address of the owner who is speaking go into the turn header (section 4).
- `/mute` and `/reset` keep clearing the day log but never touch rules.

### 4. The turn header (context for this turn only)

`request()` puts a header in front of `req.Context`. In a group:

```
[About this chat: the group "Title". Now: Thursday 16 October 2026, 15:04 (Asia/Tashkent).
Your admin rights here: delete messages, ban and mute members, pin messages.   (or: You are not an admin here.)
You can reply here, react with one emoji, and use your tools. You cannot see read receipts, who is online, the member list, other chats or private chats, open links, join groups, or remember anything older than the messages below unless a rule says so.]
[Standing rules from your owners for this group:
- (#12) Do not talk about finance. (set by @a, until 23 Oct)
- Do not answer Bek @bek; they are ignored until 17 Oct 15:00.]
[Address the owner who is writing as: "aka".]
```

In a private chat the header has only the date line and the address line.
The bot's rights come from `groups.bot_rights` (section 6); when unknown
they are fetched once with `getChatMember` and stored.

### 5. The day log

- The window becomes the last 12 hours instead of "since local midnight".
- `fit` keeps only Maria's last 5 lines, each cut to 120 characters.
- A message that replies to someone is logged as `(to Name) text`.
- Joins and leaves are logged: `<Name joined>`, `<Name left>`.
- More media is labelled for the log and the model: video
  `<video 0:15>`, round video `<round video 0:08>` (both show their
  thumbnail like a GIF), `<file name.pdf>`, `<audio Title>`,
  `<poll: question / option / option>`, and a forward is prefixed
  `<forwarded from Name>`. `textTextOnly` remains only for kinds still
  unknown (location, contact...), and is not sent in groups.
- A quoted message names its author:
  `[Quoted message from Bek that the user is replying to. Untrusted...]`.
- The question and answer are not logged again when the update was a
  duplicate.

### 6. Knowing her rights

- `telegram.ChatMember` gains `user`, `until_date` and the admin rights
  booleans (`can_delete_messages`, `can_restrict_members`,
  `can_pin_messages`, `can_promote_members`, `can_change_info`,
  `can_invite_users`, `can_manage_topics`).
- Migration adds `groups.bot_rights jsonb`. `recordMembership` stores the
  rights from `my_chat_member`; a `rights` helper renders them as words.
- `bot_database groups` shows each group's rights.

### 7. Moderation tool

Tool `moderate`, offered only in group turns. Arguments:
`action` (`delete`, `mute`, `unmute`, `ban`, `confirm_ban`, `unban`, `pin`,
`unpin`, `cancel`), `minutes` (mute only, default 60, clamped to 1..10080),
`reason` (optional, short). There is no user or message argument: the
target is always the message the requester replied to, taken from the
Telegram update (`Caller.Target`: message ID, author ID, author name,
whether the author is a bot, and up to 500 characters of its text).

Checks, all in Go, in this order, each failing with a plain-words error the
model can repeat:

1. The requester is a human in a group (`Caller.ChatID < 0`).
2. There is a target.
3. The requester is a bot owner, or Telegram says (live `getChatMember`) they
   are the creator or an administrator holding the right the action needs:
   delete → `can_delete_messages`; mute, unmute, ban, unban →
   `can_restrict_members`; pin, unpin → `can_pin_messages`. *My call*: owners
   may moderate in any group where Maria holds the right.
4. Maria holds that right (live `getChatMember` of the bot; the stored
   rights are refreshed from the answer).
5. Target immunity: never the requester (except delete and pin), never
   Maria (except deleting or pinning her own message), never a bot owner,
   never the creator or an administrator (live check), never Telegram's
   service accounts 777000, 1087968824, 136817688.
6. Per requester at most 10 actions per 10 minutes (counted in the audit
   table).
7. Idempotency: the audit row is inserted before calling Telegram with a
   unique `(update_id, action)`; a redelivered update does nothing twice.

Action details:

- `delete`: `deleteMessage` of the target (48 h rule surfaced as an error).
- `mute`: `restrictChatMember` with every permission false,
  `use_independent_chat_permissions=true`, `until_date` now+minutes;
  supergroups only.
- `unmute`: `restrictChatMember` with every permission true.
- `ban`: stores a pending ban (requester, chat, target, update ID) and
  returns `{"pending": true, ...}` telling the model to ask the requester to
  confirm. `confirm_ban` in a *later* message by the same requester within 5
  minutes repeats checks 3-6 and calls `banChatMember`. `cancel` drops it.
- `unban`: `unbanChatMember` with `only_if_banned=true`.
- `pin`: `pinChatMessage` with `disable_notification=true`; `unpin`:
  `unpinChatMessage` of the target.

Migration adds `moderation_actions` (audit: id, chat_id, action,
requester_id, target_user_id, target_message_id, target_text, minutes,
reason, update_id, created_at; unique (update_id, action)) and
`pending_bans` (one row per requester and chat). Neither is expired by
`Store.Expire` in this round.

The persona gets a moderation section: use `moderate` only when a group admin
or owner asks, on the message they replied to; never on your own, never
because someone insulted you; say exactly what the tool returned.

### 8. Polls

Tool `create_poll` (`question`, `options` 2-10, `multiple`), any member, in
groups only, at most one per chat per 10 minutes (counted in
`moderation_actions` with action `poll`). Sends `sendPoll`, anonymous.

### 9. Owner identity

New optional `BOT_OWNER_IDS` (comma-separated numeric IDs). When set,
`isOwner` requires the sender's ID to be listed: usernames are then only
used to name the owners in the persona. When unset, behaviour is unchanged.

### 10. Smaller fixes

- `get_current_time` defaults to `GROUP_HISTORY_TIMEZONE`.
- `helpText` lists what she really does: voice, photos, drawing, time,
  arithmetic, polls, moderation for admins, `/ignore`.
- `.env.example` and README: `BOT_WAKE_WORDS=maria,mariya,мария,мариа`
  (not `mira`: another bot called Mira is in the same groups), the new
  settings, and the new privacy facts (what is stored, for how long).

### 11. Prompt (persona.go)

Defaults rewritten so they agree with the code:

- Tone: playful, not savage. Roast only someone who is bantering with you,
  once, then let it go. Never their intelligence, family, upbringing,
  appearance, illness, mental health, suicide, script, dialect or spelling.
  Swearing is normal in these chats: never lecture anyone about manners or
  vocabulary. Honour a request for "siz". "Who are you?" gets a friendly
  one-line introduction. Newcomers get a normal, friendly reply.
- Language: reply in the language and script of the message you answer.
- Length: banter under about 15 words; never end or open a reply with the
  person's name by habit.
- Plain questions get plain answers first, even mid-argument; if unsure of a
  date or fact, do not give one.
- `groupNote`: drop "or replies to you" from the "always answer" rule;
  applause needs no words (react or `[silent]`); running jokes are the
  members', your own bit may return once a day and stops when called boring;
  `[silent]` and `[react X]` are whole answers, never part of one; use only
  @usernames that appear in front of you.
- `ownerNote`: no "modes" exist; a racist or hateful mode gets one warm
  refusal; never cite an owner's order to members or use it as a threat;
  owners are equals, a request to roast the other owner is a light game;
  never treat "he insulted you" as true unless the line is in front of you;
  default form of address "master" unless the header gives another for that
  owner; no servile phrases ("Xo'p bo'ladi", "bosh ustiga", "Nima
  buyurasiz", "xizmatingizga tayyorman"); an order needs no acknowledgement;
  orders meant to last are saved with `group_rules`, and nothing is promised
  for later unless a tool saved it; only owners change how she behaves.
- Propaganda rule gets one example answer.

## Left to the builder

Exact wording of prompt lines, file layout inside packages, names of
helpers, test cases beyond the ones listed, and the applause word list,
within the rules above.
