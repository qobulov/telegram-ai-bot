-- Mythic gets its first women, as the owners chose: Mikasa Ackerman, Maomao
-- and Frieren, each with a Mythic's numbers. Firefly Wedding's Satoko
-- Kirigaya, whom the players asked for, comes into a filled catalog from
-- AniList as Epic. The holders' points are counted again.
UPDATE card_catalog c
SET rarity = 4, attack = v.attack, health = v.health, value = 4000, updated_at = now()
FROM (VALUES (40881, 4590, 21570), (126824, 4660, 15510), (176754, 4490, 16710)) AS v(id, attack, health)
WHERE c.id = v.id;

INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection)
SELECT 298761, 'Satoko Kirigaya', 'Firefly Wedding', 'https://s4.anilist.co/file/anilistcdn/character/large/b298761-EU6Qgcu9AMQc.png', 276, 2, 2090, 8570, 400, 'anime'
WHERE EXISTS (SELECT 1 FROM card_catalog WHERE collection = 'anime')
ON CONFLICT (id) DO UPDATE SET rarity = EXCLUDED.rarity, attack = EXCLUDED.attack, health = EXCLUDED.health, value = EXCLUDED.value;

UPDATE card_players p
SET points = (SELECT coalesce(sum(c.value), 0)
              FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = p.user_id)
WHERE EXISTS (SELECT 1 FROM card_owned o WHERE o.user_id = p.user_id AND o.card_id IN (40881, 126824, 176754));
