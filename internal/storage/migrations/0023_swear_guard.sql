-- Owners can have a group's swearing handled in code: a first swear is
-- warned about, the next one within a day mutes for an hour. The rule is
-- one more kind of standing rule, its user_name whom the mute tags.

ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard'));
