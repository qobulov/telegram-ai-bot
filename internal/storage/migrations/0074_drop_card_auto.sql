-- Kartalar's auto opener was taken out: the day's three free cards are
-- opened by the players themselves, and nothing reads its switch any more.
ALTER TABLE card_players
    DROP COLUMN IF EXISTS auto_draw,
    DROP COLUMN IF EXISTS auto_language;
