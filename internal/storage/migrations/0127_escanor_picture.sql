-- Escanor gets the picture the owners sent on 2026-11-21, the Rhitta on his
-- shoulder and a raised finger, under a new name as art is cached for a
-- month; card_pictures keeps it over refills.
INSERT INTO card_pictures (card_id, image_url) VALUES (90202, '/kartalar/art/90202c.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
UPDATE card_catalog SET image_url = '/kartalar/art/90202c.jpg', updated_at = now() WHERE id = 90202;
