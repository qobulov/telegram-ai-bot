-- An owner's order for what the swear or picture guard does to a member, or
-- to anyone in the group: "ban" or "mute:<hours>" in text.
ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome', 'spam_guard', 'link_guard', 'image_guard',
                    'reply_limit', 'min_active', 'swear_punishment', 'picture_punishment'));
