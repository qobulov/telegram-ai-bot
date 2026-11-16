-- A player's 🎟 tokens: each opens one card, besides the day's free cards
-- and the cards bought with crystals. The keys given out at the reset are
-- given as tokens.
ALTER TABLE card_players ADD COLUMN tokens integer NOT NULL DEFAULT 0 CHECK (tokens >= 0);
