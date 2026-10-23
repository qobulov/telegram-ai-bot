-- Three more guards an owner may turn on in a group, run in code: known
-- spammers are banned (CAS, lols.bot), links to malicious hosts deleted
-- (URLhaus) and nude or gory pictures deleted (Sightengine).
ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome', 'spam_guard', 'link_guard', 'image_guard'));
