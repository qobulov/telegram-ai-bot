-- A trade's two cards may be at most one rarity apart now: a Common for a
-- Mythic was how one player's cards went to another's. Market offers made
-- before that rule which break it could be seen and never taken, so they
-- are called off.
UPDATE card_trades t
SET status = 'off', done_at = now()
FROM card_catalog g, card_catalog k
WHERE t.status = 'open' AND g.id = t.give_card AND k.id = t.take_card AND abs(g.rarity - k.rarity) > 1;
