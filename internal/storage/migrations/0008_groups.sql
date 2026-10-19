-- The groups the bot has seen a message in, for its owners' overview. Only
-- the group's name and the bot's standing are kept, so unlike the messages
-- it is not expired.

CREATE TABLE groups (
    chat_id       bigint      PRIMARY KEY,
    title         text        NOT NULL DEFAULT '',
    -- The public @username, for public groups only.
    username      text,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    -- What Telegram last said about the bot here: "member", "administrator",
    -- "left" or "kicked". NULL until it says something.
    bot_status    text,
    -- The admin rights Telegram last reported for the bot, as a JSON array of
    -- Bot API names such as "can_delete_messages", so that every turn can tell
    -- the model what it may do without asking Telegram. NULL until known; []
    -- when the bot holds none.
    bot_rights    jsonb
);
