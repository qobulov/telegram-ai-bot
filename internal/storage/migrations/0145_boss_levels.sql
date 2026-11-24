-- The level a boss fight reached, as the owners asked on 2026-11-24: each
-- fight starts the boss at level 1 and the week's table ranks the levels.
-- Fights before it reached none, 0.
ALTER TABLE boss_fights ADD COLUMN level integer DEFAULT 0 NOT NULL;
