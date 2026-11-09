-- Reminders: a member asks Maria to remind them of something at a time, or
-- when their next Kartalar card may be opened, and an endpoint called every
-- minute sends the ones that are due. A reminder is claimed once, by the
-- call that sends it, and never again, so it goes out at most once.

CREATE TABLE reminders (
    id         bigserial   PRIMARY KEY,
    chat_id    bigint      NOT NULL, -- where it is sent: the group it was asked in, or the user
    user_id    bigint      NOT NULL, -- who asked, and who is mentioned in a group
    user_name  text        NOT NULL DEFAULT '',
    message_id bigint      NOT NULL DEFAULT 0, -- the message it answers, 0 for none
    kind       text        NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'card')),
    text       text        NOT NULL DEFAULT '',
    language   text        NOT NULL DEFAULT 'uz',
    due_at     timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    attempts   integer     NOT NULL DEFAULT 0,
    claimed_at timestamptz, -- set by the call that sends it; NULL while it waits
    sent_at    timestamptz, -- when that call finished with it
    outcome    text        NOT NULL DEFAULT '' -- 'sent', 'skipped' or 'failed' once finished
);

CREATE INDEX reminders_due_idx ON reminders (due_at) WHERE claimed_at IS NULL;
CREATE INDEX reminders_waiting_idx ON reminders (user_id, chat_id) WHERE claimed_at IS NULL;
CREATE INDEX reminders_finished_idx ON reminders (claimed_at) WHERE claimed_at IS NOT NULL;
-- A player has at most one card reminder waiting.
CREATE UNIQUE INDEX reminders_one_card_idx ON reminders (user_id) WHERE kind = 'card' AND claimed_at IS NULL;

-- "Always remind me": each draw schedules the next card reminder, sent to
-- remind_chat_id in remind_language; NULL, the default, is off.
ALTER TABLE card_players
    ADD COLUMN remind_chat_id  bigint,
    ADD COLUMN remind_language text NOT NULL DEFAULT 'uz';
