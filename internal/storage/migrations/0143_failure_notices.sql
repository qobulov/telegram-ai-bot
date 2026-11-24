-- How often each chat was told, with a joke, that a reply was lost to an
-- error, a timeout or an overloaded model, as the owners asked on
-- 2026-11-24: three times at most in a run of failures, which ends when the
-- chat has seen none for an hour.
CREATE TABLE failure_notices (
    chat_id bigint PRIMARY KEY,
    warned integer DEFAULT 0 NOT NULL,
    last_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE failure_notices ENABLE ROW LEVEL SECURITY;
