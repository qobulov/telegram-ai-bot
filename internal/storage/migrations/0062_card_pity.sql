-- A sure rare card after a long wait for one: each player's draws in a row
-- since their last Legendary or better and their last Mythic, so that the
-- 30th draw without the one and the 100th without the other bring it. By the
-- odds alone 412 draws had brought no Mythic, which players took for there
-- being none. A player who holds no card of the kind waited all their draws;
-- one who holds one is counted from now, as when they drew it was not kept.
ALTER TABLE card_players
    ADD COLUMN since_legendary INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN since_mythic    INTEGER NOT NULL DEFAULT 0;

UPDATE card_players p SET
    since_legendary = CASE WHEN EXISTS (
        SELECT 1 FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
        WHERE o.user_id = p.user_id AND c.rarity >= 3) THEN 0 ELSE p.draws END,
    since_mythic = CASE WHEN EXISTS (
        SELECT 1 FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
        WHERE o.user_id = p.user_id AND c.rarity >= 4) THEN 0 ELSE p.draws END;
