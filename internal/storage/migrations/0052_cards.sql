-- Kartalar, the anime card collection: the catalog of cards, one for each
-- of AniList's most loved characters, each player's points and last draw,
-- and how many of each card they hold.

CREATE TABLE card_catalog (
    id         bigint      PRIMARY KEY, -- the character's AniList ID
    name       text        NOT NULL,
    anime      text        NOT NULL DEFAULT '',
    image_url  text        NOT NULL DEFAULT '',
    favourites integer     NOT NULL DEFAULT 0,
    rarity     smallint    NOT NULL,
    attack     integer     NOT NULL,
    health     integer     NOT NULL,
    value      integer     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX card_catalog_rarity_idx ON card_catalog (rarity);

CREATE TABLE card_players (
    user_id      bigint      PRIMARY KEY,
    name         text        NOT NULL DEFAULT '',
    points       bigint      NOT NULL DEFAULT 0,
    cards        integer     NOT NULL DEFAULT 0, -- distinct cards held
    draws        integer     NOT NULL DEFAULT 0,
    last_draw_at timestamptz
);

CREATE INDEX card_players_points_idx ON card_players (points DESC);

CREATE TABLE card_owned (
    user_id  bigint      NOT NULL,
    card_id  bigint      NOT NULL REFERENCES card_catalog (id),
    count    integer     NOT NULL DEFAULT 1,
    first_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, card_id)
);
