-- Answers the bot gave to people in groups. Counting them here, not in
-- memory, makes the group limits hold on serverless hosting, where each
-- update may be handled by a different instance.

CREATE TABLE group_replies (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX group_replies_chat_id_created_at_idx ON group_replies (chat_id, created_at);
