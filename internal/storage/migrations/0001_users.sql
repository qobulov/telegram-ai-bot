-- People who have talked to the bot.

CREATE TABLE users (
    -- The Telegram user ID. No usernames or phone numbers are stored.
    telegram_id  bigint      PRIMARY KEY,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
