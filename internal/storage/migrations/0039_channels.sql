-- The channels the bot was made an admin of, for its owners' broadcasts.
-- Telegram reports each change to the bot's standing in a channel
-- (my_chat_member), and that is all the bot hears of one: it does not ask
-- for channel posts. Kept apart from groups, which the overview, the
-- statistics and the guards count.

CREATE TABLE channels (
    chat_id       bigint      PRIMARY KEY,
    title         text        NOT NULL DEFAULT '',
    -- The public @username, for public channels only.
    username      text,
    -- "administrator", or "left" or "kicked" once removed.
    bot_status    text        NOT NULL,
    -- The bot's admin rights there, as in groups.bot_rights: a broadcast
    -- goes only where it holds can_post_messages. [] for none.
    bot_rights    jsonb       NOT NULL DEFAULT '[]',
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);

-- A broadcast may copy, as it is, a message the owner replied to instead of
-- sending text: the chat the message is in and its ID. text is '' then.
ALTER TABLE broadcasts ADD COLUMN copy_chat_id bigint;
ALTER TABLE broadcasts ADD COLUMN copy_message_id bigint;
