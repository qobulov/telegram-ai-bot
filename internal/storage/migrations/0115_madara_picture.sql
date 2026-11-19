-- Madara Uchiha gets the picture the owners sent on 2026-11-19, under a new
-- name as art is cached for a month; card_pictures keeps it over refills.
INSERT INTO card_pictures (card_id, image_url) VALUES (53901, '/kartalar/art/53901b.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
UPDATE card_catalog SET image_url = '/kartalar/art/53901b.jpg', updated_at = now() WHERE id = 53901;
