-- The flood guard, one more guard an owner may turn on in a group: a member
-- who sends many messages within seconds is muted for a while, in code.

ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome', 'spam_guard', 'link_guard', 'image_guard',
                    'reply_limit', 'min_active', 'swear_punishment', 'picture_punishment', 'join_captcha', 'flood_guard'));
