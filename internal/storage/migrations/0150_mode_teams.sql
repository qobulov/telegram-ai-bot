-- A team of its own for the boss and for the tower, as the owners asked on
-- 2026-11-25: one saved team suited neither the arena, the boss nor the
-- tower. A player without one fights there with their arena team.
CREATE TABLE mode_teams (
    user_id bigint NOT NULL,
    mode text NOT NULL CHECK (mode IN ('boss', 'tower')),
    front bigint[] NOT NULL,
    back bigint[] NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (user_id, mode)
);

ALTER TABLE mode_teams ENABLE ROW LEVEL SECURITY;
