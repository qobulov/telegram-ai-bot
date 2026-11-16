-- The Mythic cards show a portrait to animate: the head whole, with room
-- above it, the shoulders and the clothes, in colour, chosen from
-- MyAnimeList and upscaled under public/kartalar/art. Spike Spiegel had one.
INSERT INTO card_pictures (card_id, image_url)
SELECT c.id, '/kartalar/art/' || c.id || '.jpg'
FROM card_catalog c
WHERE c.id IN (40, 40882, 417, 88572, 40881, 422, 126824, 176754, 34470, 137080, 127222, 80,
    90169, 11, 17, 126071, 30, 73935, 5, 124381, 127518, 4003, 246, 72921)
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c
SET image_url = p.image_url, updated_at = now()
FROM card_pictures p
WHERE p.card_id = c.id AND p.image_url LIKE '/kartalar/art/%' AND c.image_url <> p.image_url;
