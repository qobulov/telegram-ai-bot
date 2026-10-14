-- Replies the bot gave to other bots, to stop two bots from talking to each
-- other without end.

CREATE TABLE bot_replies (
    id         bigserial   PRIMARY KEY,
    chat_id    bigint      NOT NULL,
    bot_id     bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX bot_replies_chat_id_created_at_idx ON bot_replies (chat_id, created_at);
