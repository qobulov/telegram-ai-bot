-- The messages of a conversation: what users said to the bot, what it
-- answered, and the tool calls in between.

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
    -- The Telegram message this row corresponds to: the incoming message for
    -- a user row, the bot's sent message for its answer.
    telegram_message_id bigint,
    -- The Telegram message this one replies to, if any.
    reply_to_message_id bigint,
    -- Who wrote a user message, and the first name they were shown under.
    author_id       bigint,
    author_name     text,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT messages_role_check CHECK (role IN ('user', 'assistant', 'tool')),
    CONSTRAINT messages_tool_calls_check CHECK (tool_calls IS NULL OR role = 'assistant'),
    CONSTRAINT messages_tool_call_id_check CHECK ((role = 'tool') = (tool_call_id IS NOT NULL))
);

-- Loading the most recent messages of a conversation.
CREATE INDEX messages_conversation_id_id_idx ON messages (conversation_id, id);

-- Each Telegram update is stored at most once.
CREATE UNIQUE INDEX messages_update_id_key ON messages (update_id) WHERE update_id IS NOT NULL;
