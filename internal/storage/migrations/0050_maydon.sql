-- Each group's Maydon room, the battle being fought or the last: its
-- fighters, their classes and levels, the battle's state and its last hits,
-- kept as JSON by internal/maydon and changed with the row locked.

CREATE TABLE maydon_rooms (
    chat_id    bigint      PRIMARY KEY,
    state      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
