-- Cards whose picture hid the character (a back turned, a dark crop, a
-- manga panel) show a clear colour portrait from MyAnimeList, upscaled
-- under public/kartalar/art: Spike Spiegel, Ken Kaneki, Aki Hayakawa, Maki
-- Zenin, Yato, Griffith, Sousuke Aizen and Yoruichi Shihouin. The other ten
-- replaced kept their art path, so only the file changed.
INSERT INTO card_pictures (card_id, image_url)
SELECT c.id, '/kartalar/art/' || c.id || '.jpg'
FROM card_catalog c
WHERE c.id IN (1, 87275, 137081, 134167, 84677, 424, 1086, 908)
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c
SET image_url = p.image_url, updated_at = now()
FROM card_pictures p
WHERE p.card_id = c.id AND p.image_url LIKE '/kartalar/art/%' AND c.image_url <> p.image_url;
