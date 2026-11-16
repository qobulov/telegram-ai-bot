-- Each member's own invite link the bot made for them, /mylink: whoever
-- joins by it is counted for that member, not for the link's maker.
CREATE TABLE member_links (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    link       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);
