-- What the picture guard decided about each picture it looked at, by
-- Telegram's file_unique_id, which is the same for a file in every chat:
-- a sticker or meme sent again, in any group, on any instance or after a
-- restart, is never sent to Sightengine or the model a second time.
CREATE TABLE picture_verdicts (
    file_unique_id text        PRIMARY KEY,
    unsafe         boolean     NOT NULL,
    -- Why it is unsafe, as the notice gives it ("nudity"); empty when safe.
    reason         text        NOT NULL DEFAULT '',
    -- Who judged it: 'sightengine' or 'model'.
    judge          text        NOT NULL CHECK (judge IN ('sightengine', 'model')),
    created_at     timestamptz NOT NULL DEFAULT now()
);
