-- The arena's weekly prizes, as the owners asked on 2026-11-19: each week,
-- Monday to Monday in Tashkent, the top 100 players by rating who attacked
-- that week get tokens by mail. A week is noted here when it is paid, so
-- that it is paid once.
CREATE TABLE arena_weeks (
    week    DATE        PRIMARY KEY,
    players INT         NOT NULL DEFAULT 0,
    paid_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
