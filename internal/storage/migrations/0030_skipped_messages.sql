-- Messages addressed to the bot that it did not answer, and why, so that
-- "why does she not answer?" is read from the database, not guessed.

CREATE TABLE skipped_messages (
    id         bigserial   PRIMARY KEY,
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    message_id bigint      NOT NULL,
    reason     text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX skipped_messages_created_at_idx ON skipped_messages (created_at);
