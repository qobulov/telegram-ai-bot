-- The 🧱 materials a player holds, as the owners asked on 2026-11-24: the
-- spare copies of Common and Rare cards break into them, and they buy
-- boxes of an Epic, a Legendary or a Mythic card.
ALTER TABLE card_players ADD COLUMN materials integer DEFAULT 0 NOT NULL CHECK (materials >= 0);
