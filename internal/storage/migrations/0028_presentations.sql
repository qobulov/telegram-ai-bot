-- Presentations made for each user, to apply the daily limit.

CREATE TABLE presentations (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL,
    chat_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX presentations_user_id_created_at_idx ON presentations (user_id, created_at);
