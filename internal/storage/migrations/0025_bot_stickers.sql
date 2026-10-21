-- Stickers Maria may send, from packs an owner imported. The model only
-- names a category; the code picks a sticker of it here. Each sticker's
-- categories and a short description come from the model looking at it
-- once, at import; a sticker it found unfit is kept but not enabled.

CREATE TABLE bot_stickers (
    -- Telegram's lasting ID of the sticker; file_id may change.
    file_unique_id text        PRIMARY KEY,
    file_id        text        NOT NULL,
    pack           text        NOT NULL,
    emoji          text        NOT NULL DEFAULT '',
    -- What shows it as a picture for the model: the sticker when static,
    -- its thumbnail when it moves.
    image_file_id  text        NOT NULL DEFAULT '',
    categories     text[]      NOT NULL DEFAULT '{}',
    description    text        NOT NULL DEFAULT '',
    labelled       boolean     NOT NULL DEFAULT false,
    enabled        boolean     NOT NULL DEFAULT false,
    used_count     integer     NOT NULL DEFAULT 0,
    last_used_at   timestamptz
);

CREATE INDEX bot_stickers_categories_idx ON bot_stickers USING GIN (categories);

-- When each chat last got a sticker, so that stickers come seldom.
CREATE TABLE sticker_sends (
    chat_id bigint      PRIMARY KEY,
    sent_at timestamptz NOT NULL
);
