-- Farosat, a daily game in groups, as the owners asked on 2026-11-26, to
-- take the place of another bot's: once a day a member's farosat ("sense")
-- grows, or now and then shrinks, by some grams, and the group ranks them.
CREATE TABLE IF NOT EXISTS farosat (
    chat_id    bigint NOT NULL,
    user_id    bigint NOT NULL,
    name       text NOT NULL DEFAULT '',
    grams      integer NOT NULL DEFAULT 0,
    last_day   date,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS farosat_rank ON farosat (chat_id, grams DESC);
