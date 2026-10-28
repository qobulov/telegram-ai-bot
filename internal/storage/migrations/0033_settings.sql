-- Owners' settings over the environment's, by the environment variable's
-- name: chat_id 0 holds for the whole bot, a group's for that group alone.
-- The value is checked as the environment's would be before it is saved.
CREATE TABLE settings (
    chat_id    bigint      NOT NULL DEFAULT 0,
    name       text        NOT NULL,
    value      text        NOT NULL,
    set_by     bigint      NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, name)
);

-- The group settings /limit and /active kept as group rules move here.
INSERT INTO settings (chat_id, name, value, set_by, updated_at)
SELECT chat_id,
       CASE kind WHEN 'reply_limit' THEN 'GROUP_RATE_LIMIT_PER_MINUTE' ELSE 'GROUP_MIN_ACTIVE_MEMBERS' END,
       text, set_by, created_at
FROM group_rules
WHERE kind IN ('reply_limit', 'min_active')
ON CONFLICT (chat_id, name) DO NOTHING;

DELETE FROM group_rules WHERE kind IN ('reply_limit', 'min_active');
