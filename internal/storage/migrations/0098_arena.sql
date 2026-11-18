-- The arena: each player's saved team of five, two in front and three
-- behind; their rating and the day's attacks and tokens won, counted by
-- Tashkent date; and every battle fought, with the seed that replays it.
CREATE TABLE arena_teams (
    user_id    BIGINT      PRIMARY KEY,
    front      BIGINT[]    NOT NULL,
    back       BIGINT[]    NOT NULL,
    power      INT         NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE arena_players (
    user_id     BIGINT PRIMARY KEY,
    rating      INT    NOT NULL DEFAULT 1000,
    wins        INT    NOT NULL DEFAULT 0,
    losses      INT    NOT NULL DEFAULT 0,
    attacks_day DATE,
    attacks     INT    NOT NULL DEFAULT 0,
    tokens_day  DATE,
    tokens_won  INT    NOT NULL DEFAULT 0
);

CREATE INDEX arena_players_rating_idx ON arena_players (rating);

CREATE TABLE arena_battles (
    id            BIGSERIAL   PRIMARY KEY,
    attacker      BIGINT      NOT NULL,
    defender      BIGINT      NOT NULL,
    seed          BIGINT      NOT NULL,
    attacker_won  BOOLEAN     NOT NULL,
    rating_change INT         NOT NULL,
    at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
