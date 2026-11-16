-- Satoru Gojo is Mythic, as the owners asked: 4000 points, his attack and
-- life as a Mythic's, and a portrait of his own under public/kartalar/art.
-- The players who hold him have their points counted again.
UPDATE card_catalog SET rarity = 4, attack = 4500, health = 21890, value = 4000, updated_at = now()
WHERE id = 127691;

INSERT INTO card_pictures (card_id, image_url)
SELECT id, '/kartalar/art/127691.jpg' FROM card_catalog WHERE id = 127691
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id = 127691;

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned WHERE card_id = 127691);
