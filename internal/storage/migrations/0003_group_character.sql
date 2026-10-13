-- A group now has one conversation shared by all its members, and the bot
-- can be muted per chat.

-- The shared conversation of a group belongs to no single user, so it has no
-- user_id. Private conversations keep theirs.
ALTER TABLE conversations ALTER COLUMN user_id DROP NOT NULL;

-- At most one shared conversation per chat.
CREATE UNIQUE INDEX conversations_group_key ON conversations (chat_id) WHERE user_id IS NULL;

ALTER TABLE messages
    -- Who wrote a user message, and the first name they were shown under.
    ADD COLUMN author_id   bigint,
    ADD COLUMN author_name text;

-- Per-chat settings. A row exists only for chats where something was set.
CREATE TABLE chats (
    chat_id    bigint      PRIMARY KEY,
    -- While true the bot does not react in this chat.
    muted      boolean     NOT NULL DEFAULT false,
    -- The user who last changed the setting.
    updated_by bigint,
    updated_at timestamptz NOT NULL DEFAULT now()
);
