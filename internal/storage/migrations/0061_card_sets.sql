-- The version of the code each trading collection was last filled by: one that
-- reads a set better (all its pages, one card per character and rarity) adds
-- what the old one missed, the Pokémon set's last 42 cards among them, and
-- moves the holders of an old art variant to the card kept. A collection with
-- no row was filled before.
CREATE TABLE card_sets (
    collection TEXT PRIMARY KEY,
    version    INTEGER NOT NULL,
    filled_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
