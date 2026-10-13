-- Initial schema: users, one conversation per user, and its messages.

CREATE TABLE users (
    -- The Telegram user ID. No names or usernames are stored.
    telegram_id  bigint      PRIMARY KEY,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversations (
    id         bigserial   PRIMARY KEY,
    -- UNIQUE: a user has at most one conversation. /reset deletes it.
    user_id    bigint      NOT NULL UNIQUE REFERENCES users (telegram_id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Used by the retention job to find idle conversations.
CREATE INDEX conversations_updated_at_idx ON conversations (updated_at);

CREATE TABLE messages (
    id              bigserial   PRIMARY KEY,
    conversation_id bigint      NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            text        NOT NULL,
    content         text        NOT NULL DEFAULT '',
    -- Tool calls requested by the model, as sent by the API. Assistant messages only.
    tool_calls      jsonb,
    -- Which tool call this message answers. Tool messages only.
    tool_call_id    text,
    -- The Telegram update that produced a user message; used to skip redelivered updates.
    update_id       bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT messages_role_check CHECK (role IN ('user', 'assistant', 'tool')),
    CONSTRAINT messages_tool_calls_check CHECK (tool_calls IS NULL OR role = 'assistant'),
    CONSTRAINT messages_tool_call_id_check CHECK ((role = 'tool') = (tool_call_id IS NOT NULL))
);

-- Loading the most recent messages of a conversation.
CREATE INDEX messages_conversation_id_id_idx ON messages (conversation_id, id);

-- Each Telegram update is stored at most once.
CREATE UNIQUE INDEX messages_update_id_key ON messages (update_id) WHERE update_id IS NOT NULL;
