-- Owners' standing orders, kept apart from the conversation so that /mute
-- and /reset, which clear the history, never erase them.

CREATE TABLE group_rules (
    id         bigserial   PRIMARY KEY,
    -- The group the rule is for; 0 for a rule that holds everywhere.
    chat_id    bigint      NOT NULL,
    kind       text        NOT NULL CHECK (kind IN ('instruction', 'ignore', 'address')),
    -- The ignored member, or the owner a form of address is for.
    user_id    bigint,
    user_name  text        NOT NULL DEFAULT '',
    -- The instruction, or the form of address.
    text       text        NOT NULL DEFAULT '',
    set_by     bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- NULL keeps the rule until it is removed.
    expires_at timestamptz
);

CREATE INDEX group_rules_chat_id_idx ON group_rules (chat_id);
