-- Escanor shows one picture, the golden armour under the lion of the sun,
-- in the small tile and the large view alike, as the owners asked on
-- 2026-11-21; card_pictures keeps it over refills.
INSERT INTO card_pictures (card_id, image_url) VALUES (90202, '/kartalar/art/90202L.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
UPDATE card_catalog SET image_url = '/kartalar/art/90202L.jpg', updated_at = now() WHERE id = 90202;
