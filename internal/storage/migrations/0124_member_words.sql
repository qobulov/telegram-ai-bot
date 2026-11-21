-- How many words each member writes in a group a day, for /words, the
-- owners asked on 2026-11-21: the top writers of the last 7 and 30 days.
-- group_messages cannot say it: a group without AI keeps no words there.
-- The retention job deletes days older than RETENTION_DAYS.

CREATE TABLE member_words (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    day date NOT NULL,
    name text NOT NULL DEFAULT '',
    words integer NOT NULL DEFAULT 0,
    messages integer NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_id, day, user_id)
);

ALTER TABLE member_words ENABLE ROW LEVEL SECURITY;
