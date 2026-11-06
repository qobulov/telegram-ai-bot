-- Each group's Mafia game, the one being played or the last: its players,
-- their secret roles, the night's acts, the votes and what the checkers
-- found, kept as JSON by internal/mafia and changed with the row locked.

CREATE TABLE mafia_games (
    chat_id    bigint      PRIMARY KEY,
    state      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
