-- Who brought whom into a group, for /qoshganlar: a member added by another
-- (the message that says they joined is from the one who added them), or one
-- who joined by another's own invite link, which Telegram tells a bot that is
-- an admin. A person is counted once for each who brought them, so leaving and
-- coming back adds nothing.
CREATE TABLE group_invites (
    chat_id      BIGINT      NOT NULL,
    user_id      BIGINT      NOT NULL,
    inviter_id   BIGINT      NOT NULL,
    inviter_name TEXT        NOT NULL DEFAULT '',
    via          TEXT        NOT NULL CHECK (via IN ('add', 'link')),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id, inviter_id)
);

CREATE INDEX group_invites_chat_idx ON group_invites (chat_id, joined_at);
