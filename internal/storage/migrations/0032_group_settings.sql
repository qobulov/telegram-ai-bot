-- An owner's settings for one group, the number in text: the answers a
-- minute the bot gives there, and how many members must have written lately
-- for it to answer at all (0 for none). Without one the configured default
-- holds.
ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome', 'spam_guard', 'link_guard', 'image_guard',
                    'reply_limit', 'min_active'));
