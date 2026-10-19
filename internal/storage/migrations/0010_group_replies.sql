-- Answers the bot gave to people in groups. Counting them here, not in
-- memory, makes the group limits hold on serverless hosting, where each
-- update may be handled by a different instance.

CREATE TABLE group_replies (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- An answer reserved for a turn that then answered nothing, such as an
    -- update Telegram delivered again, is given back by this ID. Last, as in
    -- databases where it was added later.
    id         bigserial   PRIMARY KEY
);

CREATE INDEX group_replies_chat_id_created_at_idx ON group_replies (chat_id, created_at);
