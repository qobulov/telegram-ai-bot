-- The boosts given to groups, kept only so that each is thanked once:
-- Telegram sends a boost again when it changes, such as when it is
-- renewed, and may deliver one update twice. Who gave it is not kept.
CREATE TABLE group_boosts (
    chat_id    bigint      NOT NULL,
    boost_id   text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, boost_id)
);
