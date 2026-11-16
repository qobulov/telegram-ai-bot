-- A member's warnings in a group: an admin warns them, and at the third
-- the bot mutes them for a day and the count starts again.
CREATE TABLE member_warns (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    count      integer     NOT NULL DEFAULT 0 CHECK (count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);
