-- Answers the bot gave each user in private chats, to apply the daily limit,
-- and one denied row per user and day for the notice that the limit was
-- reached, so that it is sent once. Counting them here, not in memory, makes
-- the limit hold on serverless hosting.

CREATE TABLE private_messages (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL,
    denied     boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX private_messages_user_id_created_at_idx ON private_messages (user_id, created_at);
