-- One conversation per private chat, and one per group.

CREATE TABLE conversations (
    id         bigserial   PRIMARY KEY,
    -- The Telegram chat. In a private chat it equals the user's ID.
    chat_id    bigint      NOT NULL,
    -- The owner of a private conversation. NULL for a group, whose
    -- conversation is shared by all its members.
    user_id    bigint      REFERENCES users (telegram_id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- At most one conversation per user in a chat.
    CONSTRAINT conversations_chat_id_user_id_key UNIQUE (chat_id, user_id)
);

-- At most one shared conversation per group. The constraint above cannot do
-- this, because NULL user IDs never count as equal to each other.
CREATE UNIQUE INDEX conversations_group_key ON conversations (chat_id) WHERE user_id IS NULL;

-- Serves the foreign key to users.
CREATE INDEX conversations_user_id_idx ON conversations (user_id);

-- Used by the retention job to find idle conversations.
CREATE INDEX conversations_updated_at_idx ON conversations (updated_at);
