-- Nezuko gets the picture the owners sent on 2026-11-21 (in her demon form
-- under a full moon), under a new name as art is cached for a month, with its
-- cut-out and plate under art/cut and art/plate; the app gives her a pink
-- blood effect (SPECIAL's fx).
INSERT INTO card_pictures (card_id, image_url) VALUES (127518, '/kartalar/art/127518b.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog SET image_url = '/kartalar/art/127518b.jpg', updated_at = now() WHERE id = 127518;
