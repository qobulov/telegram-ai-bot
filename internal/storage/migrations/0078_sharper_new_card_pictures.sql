-- The cards added since the pictures were sharpened, The Seven Deadly Sins'
-- Elizabeth, Merlin, King, Diane, Zeldris, Mael and Gowther and Firefly
-- Wedding's Satoko Kirigaya, show their AniList pictures upscaled the same
-- way, kept under public/kartalar/art.
INSERT INTO card_pictures (card_id, image_url)
SELECT c.id, '/kartalar/art/' || c.id || '.jpg'
FROM card_catalog c
WHERE c.image_url LIKE '%anilist%' AND c.id IN (72923, 89180, 83801, 74653, 131559, 126851, 88823, 298761)
ON CONFLICT (card_id) DO NOTHING;

UPDATE card_catalog c
SET image_url = p.image_url, updated_at = now()
FROM card_pictures p
WHERE p.card_id = c.id AND p.image_url LIKE '/kartalar/art/%' AND c.image_url <> p.image_url;
