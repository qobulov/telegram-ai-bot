-- Each group's MLBB team picker: the lobby /teams opened last, the members
-- who joined it and, once split, its two teams of five and the bench, kept
-- as JSON by internal/service and changed with the row locked, so that
-- presses at once neither lose a member nor split twice.
CREATE TABLE team_lobbies (
    chat_id    BIGINT      PRIMARY KEY,
    state      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
