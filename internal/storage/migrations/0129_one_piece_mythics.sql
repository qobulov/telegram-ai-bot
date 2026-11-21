-- Two Mythics for One Piece, as the owners asked on 2026-11-21: Joy Boy and
-- Imu join the catalog as Mythics, with cards.CardOf's Mythic attack, life
-- and 4000 points, a 600x900 picture (Imu's the one the owners sent) and a
-- cut-out under art/cut so that they breathe, kept in card_pictures.
INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value) VALUES
    (341936, 'Joy Boy', 'ONE PIECE', '/kartalar/art/341936m.jpg', 37, 4, 4230, 19440, 4000),
    (128130, 'Imu', 'ONE PIECE', '/kartalar/art/128130m.jpg', 120, 4, 5030, 20190, 4000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (341936, '/kartalar/art/341936m.jpg'),
    (128130, '/kartalar/art/128130m.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
