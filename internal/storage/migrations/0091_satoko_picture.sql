-- Satoko Kirigaya shows a coloured frame from the anime, as the owners
-- chose, in place of the black and white one: under a new name, so that a
-- phone keeping the old picture for a month loads the new one.
INSERT INTO card_pictures (card_id, image_url)
SELECT id, '/kartalar/art/298761b.jpg' FROM card_catalog WHERE id = 298761
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id = 298761;
