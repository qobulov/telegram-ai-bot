-- The groups the bot has seen a message in, for its owners' overview. Only
-- the group's name is kept, so unlike the messages it is not expired.

CREATE TABLE groups (
    chat_id       bigint      PRIMARY KEY,
    title         text        NOT NULL DEFAULT '',
    -- The public @username, for public groups only.
    username      text,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);
