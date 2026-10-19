-- The members the bot has seen in each group: by their messages, by joining
-- or leaving, and by Telegram's chat_member updates. The Bot API cannot list
-- a group's members, so this is the only list there is. No message text is
-- kept; members who left are forgotten after 90 days.

CREATE TABLE group_members (
    chat_id      bigint      NOT NULL,
    user_id      bigint      NOT NULL,
    first_name   text        NOT NULL DEFAULT '',
    username     text        NOT NULL DEFAULT '',
    is_bot       boolean     NOT NULL DEFAULT false,
    -- Telegram's word: creator, administrator, member, restricted, left or kicked.
    status       text        NOT NULL DEFAULT 'member',
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX group_members_chat_id_last_seen_at_idx ON group_members (chat_id, last_seen_at DESC);
