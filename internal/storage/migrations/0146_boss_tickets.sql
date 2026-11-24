-- The boss's fights as tickets, as the owners asked on 2026-11-24: up to 10
-- held, one back each hour, each fight taking one. A player without a row
-- holds them all; tickets_at is when the count was made, null when full.
CREATE TABLE boss_tickets (
    user_id bigint PRIMARY KEY,
    tickets integer NOT NULL,
    tickets_at timestamp with time zone
);

ALTER TABLE boss_tickets ENABLE ROW LEVEL SECURITY;
