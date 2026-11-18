-- How many copies of each card were ever drawn, so that a draw can say which
-- copy in the whole game it gave: it only grows, melting or trading copies
-- does not take it back. Backfilled from the copies held now.
ALTER TABLE card_catalog ADD COLUMN drawn INT NOT NULL DEFAULT 0;
UPDATE card_catalog c SET drawn = coalesce((SELECT sum(count) FROM card_owned o WHERE o.card_id = c.id), 0);
