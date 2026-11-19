-- New pictures for three Mythic cards, as the owners chose on 2026-11-19:
-- Guts gets a front-facing bust under a new name (art is cached for a
-- month, and the app names the cut-out for the picture), and Tristan
-- Liones and Lancelot, who showed AniList's pictures, get pictures of their
-- own with cut-outs. card_pictures keeps them over the catalog's refills.
INSERT INTO card_pictures (card_id, image_url) VALUES
    (422, '/kartalar/art/422b.jpg'),
    (249281, '/kartalar/art/249281.jpg'),
    (265209, '/kartalar/art/265209.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (422, 249281, 265209);
