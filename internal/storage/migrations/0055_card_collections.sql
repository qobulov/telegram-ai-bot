-- Kartalar's cards come in collections now: AniList's anime characters and
-- PokéWallet's trading cards, one for each game.

ALTER TABLE card_catalog ADD COLUMN collection text NOT NULL DEFAULT 'anime';
