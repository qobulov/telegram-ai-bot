-- Firefly Wedding: Satoko Kirigaya becomes Mythic, as the owners asked,
-- with a brighter picture of her under a new name (a phone keeps the old one
-- for a month) and her cut-out for the Mythic cards' moving layer
-- (/kartalar/art/cut/298761.webp); her numbers are the code's for a Mythic
-- (CardOf). Shinpei Gotou, the series' other lead, joins as Epic, with
-- AniList's picture.
UPDATE card_catalog SET rarity = 4, attack = 5940, health = 18620, value = 4000
WHERE id = 298761;

INSERT INTO card_pictures (card_id, image_url)
SELECT id, '/kartalar/art/298761c.jpg' FROM card_catalog WHERE id = 298761
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id = 298761;

INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection) VALUES
    (298762, 'Shinpei Gotou', 'Firefly Wedding', 'https://s4.anilist.co/file/anilistcdn/character/large/b298762-GJdjyPmhS8Ln.png', 575, 2, 1800, 8040, 400, 'anime')
ON CONFLICT (id) DO NOTHING;
