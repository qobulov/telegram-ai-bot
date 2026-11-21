-- Arena players whose rating battles do not move, as the owners asked on
-- 2026-11-21: set by hand for players the owners name, who still attack,
-- defend and win or lose, while their opponents' ratings move as before.
ALTER TABLE arena_players
    ADD COLUMN rating_frozen boolean DEFAULT false NOT NULL;
