-- Kartalar's market: a player puts a card of theirs up for one they want,
-- and anyone who holds that card may take the offer, each giving the other
-- their card. An offer is a card_trades row with no to_id until it is taken.
ALTER TABLE card_trades ALTER COLUMN to_id DROP NOT NULL;

CREATE INDEX card_trades_market_idx ON card_trades (created_at DESC) WHERE status = 'open' AND to_id IS NULL;
