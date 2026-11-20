-- How often each chat was told the AI's credits or quota ran out, as the
-- owners asked on 2026-11-20: three times at most for one outage, which
-- ends when the chat has seen no such failure for a while.
CREATE TABLE ai_limit_notices (
    chat_id bigint PRIMARY KEY,
    warned integer DEFAULT 0 NOT NULL,
    last_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ai_limit_notices ENABLE ROW LEVEL SECURITY;
