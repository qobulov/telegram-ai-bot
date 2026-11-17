-- Alisa and Mariya Kujou and Yuki Suou, of Alya Sometimes Hides Her
-- Feelings in Russian, show the frames the owners chose, under new names so
-- that a phone keeping the old pictures loads these.
INSERT INTO card_pictures (card_id, image_url)
SELECT id, '/kartalar/art/' || id || 'b.jpg' FROM card_catalog WHERE id IN (217334, 291294, 243404)
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (217334, 291294, 243404);
