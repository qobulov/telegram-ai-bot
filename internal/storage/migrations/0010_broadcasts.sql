-- Messages an owner asked the bot to post in its groups. A draft is sent only
-- when the owner confirms it in a later message.

CREATE TABLE broadcasts (
    id         bigserial   PRIMARY KEY,
    owner_id   bigint      NOT NULL,
    -- The Telegram update the draft was made in; it cannot confirm itself.
    update_id  bigint      NOT NULL,
    text       text        NOT NULL,
    chat_ids   bigint[]    NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Set when the owner confirmed and sending began.
    sent_at    timestamptz
);

CREATE INDEX broadcasts_owner_id_idx ON broadcasts (owner_id, id);
