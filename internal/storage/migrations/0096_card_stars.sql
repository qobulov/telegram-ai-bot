-- Fusion: copies of a card a player holds raise it, ★1 to ★5. stars is the
-- card's level with its holder; the copies used up for it are counted in
-- melted, as the melted ones were.
ALTER TABLE card_owned ADD COLUMN stars SMALLINT NOT NULL DEFAULT 1 CHECK (stars BETWEEN 1 AND 5);
