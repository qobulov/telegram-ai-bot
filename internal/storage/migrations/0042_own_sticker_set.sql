-- Maria's own sticker set, which her owners fill from the chat. Telegram
-- makes a set a bot creates belong to a user, and every sticker added later
-- is added as that user, so the owner who made it is remembered. The name
-- ends in the bot's username, so each bot sharing the database has its own.

CREATE TABLE own_sticker_sets (
    name       text        PRIMARY KEY,
    owner_id   bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Stickers each owner added to it, to hold them to a number a day.
CREATE TABLE sticker_uploads (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL,
    chat_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sticker_uploads_user_id_created_at_idx ON sticker_uploads (user_id, created_at);
