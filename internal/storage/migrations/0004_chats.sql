-- Per-chat settings. A row exists only for chats where something was set.

CREATE TABLE chats (
    chat_id    bigint      PRIMARY KEY,
    -- While true the bot does not react in this chat.
    muted      boolean     NOT NULL DEFAULT false,
    -- The user who last changed the setting.
    updated_by bigint,
    updated_at timestamptz NOT NULL DEFAULT now()
);
