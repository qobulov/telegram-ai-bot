-- Shashka, the bot's draughts Mini App: each player's rating and record,
-- each game with its position, and the players waiting for a random
-- opponent. A game is played by two people, or one and the bot (side 0),
-- and is changed only at the version it was read at, so two presses at once
-- cannot both move.

CREATE TABLE shashka_players (
    user_id    bigint      PRIMARY KEY,
    name       text        NOT NULL DEFAULT '',
    -- language is their Telegram app's, for the messages the bot sends them.
    language   text        NOT NULL DEFAULT '',
    rating     integer     NOT NULL DEFAULT 1200,
    wins       integer     NOT NULL DEFAULT 0,
    losses     integer     NOT NULL DEFAULT 0,
    draws      integer     NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shashka_players_rating_idx ON shashka_players (rating DESC) WHERE wins + losses + draws > 0;

CREATE TABLE shashka_games (
    id           bigserial   PRIMARY KEY,
    -- kind is 'bot', 'friend' or 'random'; level is the bot's, 0 otherwise.
    kind         text        NOT NULL,
    level        smallint    NOT NULL DEFAULT 0,
    white_id     bigint      NOT NULL DEFAULT 0,
    black_id     bigint      NOT NULL DEFAULT 0,
    position     text        NOT NULL,
    -- status is 'waiting' (an invite no one took yet), 'playing' or 'over'.
    status       text        NOT NULL,
    last_path    integer[]   NOT NULL DEFAULT '{}',
    captured     integer[]   NOT NULL DEFAULT '{}',
    version      integer     NOT NULL DEFAULT 0,
    invite_code  text        UNIQUE,
    rated        boolean     NOT NULL DEFAULT false,
    -- deadline is when the side to move loses on time; null without a clock.
    deadline     timestamptz,
    -- winner is 1 for white, -1 for black, 0 for a draw or a game not over.
    winner       smallint    NOT NULL DEFAULT 0,
    reason       text        NOT NULL DEFAULT '',
    white_change integer     NOT NULL DEFAULT 0,
    black_change integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shashka_games_white_open_idx ON shashka_games (white_id) WHERE status <> 'over';
CREATE INDEX shashka_games_black_open_idx ON shashka_games (black_id) WHERE status <> 'over';
CREATE INDEX shashka_games_updated_at_idx ON shashka_games (updated_at);

CREATE TABLE shashka_queue (
    user_id   bigint      PRIMARY KEY,
    rating    integer     NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now()
);
