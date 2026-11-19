-- Anime collection rewards, as the owners asked on 2026-11-19: a player who
-- holds every card of an anime (seasons and sequels counted as one, limited
-- cards left out) that has at least five cards claims tokens for it once.
CREATE TABLE card_anime_rewards (
    user_id    BIGINT      NOT NULL,
    anime      TEXT        NOT NULL,
    tokens     INT         NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, anime)
);
