-- The Tower of Souls, as the owners asked on 2026-11-19: a player's highest
-- floor cleared and the day's attempts, counted by Tashkent's date.
CREATE TABLE tower_players (
    user_id bigint PRIMARY KEY,
    floor integer DEFAULT 0 NOT NULL,
    tries_day date,
    tries integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX tower_players_floor_idx ON tower_players USING btree (floor DESC);

ALTER TABLE tower_players ENABLE ROW LEVEL SECURITY;
