-- The boss's weekly prizes, as the owners asked on 2026-11-23: a row for
-- each Tashkent week paid, so that it is paid once, and how many were paid.
CREATE TABLE boss_weeks (
    week date PRIMARY KEY,
    players integer DEFAULT 0 NOT NULL,
    paid_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE boss_weeks ENABLE ROW LEVEL SECURITY;
