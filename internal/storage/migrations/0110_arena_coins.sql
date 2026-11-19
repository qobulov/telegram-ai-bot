-- Arena coins, as the owners asked on 2026-11-19: each attack gives the
-- attacker coins, won or lost, spent in the arena's shop on tokens (a few
-- a day, counted by shop_day and shop_tokens) and packs of cards.
ALTER TABLE arena_players
    ADD COLUMN coins integer DEFAULT 0 NOT NULL,
    ADD COLUMN shop_day date,
    ADD COLUMN shop_tokens integer DEFAULT 0 NOT NULL;
