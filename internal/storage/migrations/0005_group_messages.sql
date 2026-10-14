-- Every message seen in a group, so the bot can follow the day's
-- conversation. Kept for RETENTION_DAYS like the rest of the history.

CREATE TABLE group_messages (
    id          bigserial   PRIMARY KEY,
    chat_id     bigint      NOT NULL,
    -- The Telegram message, when known.
    message_id  bigint,
    -- Who wrote it, and the name shown for them.
    author_id   bigint,
    author_name text        NOT NULL,
    text        text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Reading a group's messages since a moment, and expiring old ones.
CREATE INDEX group_messages_chat_id_created_at_idx ON group_messages (chat_id, created_at);
CREATE INDEX group_messages_created_at_idx ON group_messages (created_at);
