-- Kartalar's cards come from AniList alone now: the trading card collections
-- (Pokémon, One Piece, Dragon Ball, Yu-Gi-Oh!, Digimon, from PokéWallet) are
-- removed. Each trading card a player held becomes an anime card of the same
-- rarity, with its copies and melted copies, those the player does not have
-- yet first, so that, a rarity being worth the same in every collection, no
-- one's points change and a card keeps a copy not melted. The players' points
-- and counts of cards are then counted again from what they hold.

CREATE TEMPORARY TABLE trading_held ON COMMIT DROP AS
SELECT o.user_id, c.rarity, o.count, o.melted,
       row_number() OVER (PARTITION BY o.user_id, c.rarity ORDER BY o.card_id) AS k
FROM card_owned o
JOIN card_catalog c ON c.id = o.card_id AND c.collection <> 'anime';

CREATE TEMPORARY TABLE anime_pool ON COMMIT DROP AS
SELECT u.user_id, a.rarity, a.id,
       row_number() OVER (PARTITION BY u.user_id, a.rarity
                          ORDER BY o.card_id IS NOT NULL, md5(u.user_id::text || ':' || a.id::text)) AS k,
       count(*) OVER (PARTITION BY u.user_id, a.rarity) AS size
FROM (SELECT DISTINCT user_id FROM trading_held) u
CROSS JOIN card_catalog a
LEFT JOIN card_owned o ON o.user_id = u.user_id AND o.card_id = a.id
WHERE a.collection = 'anime';

INSERT INTO card_owned (user_id, card_id, count, melted)
SELECT t.user_id, p.id, sum(t.count), sum(t.melted)
FROM trading_held t
JOIN anime_pool p ON p.user_id = t.user_id AND p.rarity = t.rarity AND p.k = (t.k - 1) % p.size + 1
GROUP BY t.user_id, p.id
ON CONFLICT (user_id, card_id) DO UPDATE
SET count = card_owned.count + EXCLUDED.count, melted = card_owned.melted + EXCLUDED.melted;

DELETE FROM card_trades t USING card_catalog c
WHERE c.collection <> 'anime' AND (c.id = t.give_card OR c.id = t.take_card);
DELETE FROM card_owned o USING card_catalog c WHERE c.id = o.card_id AND c.collection <> 'anime';
DELETE FROM card_pictures p USING card_catalog c WHERE c.id = p.card_id AND c.collection <> 'anime';
DELETE FROM card_catalog WHERE collection <> 'anime';
DROP TABLE card_sets;

UPDATE card_players p
SET cards  = (SELECT count(*) FROM card_owned o WHERE o.user_id = p.user_id),
    points = (SELECT coalesce(sum(o.count * c.value), 0)
              FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = p.user_id);
