-- Levi is Mythic, as the owners asked on 2026-11-21: a Mythic's attack, life
-- and 4000 points (cards.CardOf), and his holders' points counted again; he,
-- Mikasa and Eren get the pictures the owners sent, under new names as art is
-- cached for a month, with cut-outs under art/cut so that they breathe.
UPDATE card_catalog SET rarity = 4, attack = 4450, health = 15110, value = 4000, updated_at = now() WHERE id = 45627;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (45627, '/kartalar/art/45627m.jpg'),
    (40881, '/kartalar/art/40881b.jpg'),
    (40882, '/kartalar/art/40882b.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url, updated_at = now()
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (45627, 40881, 40882);

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned WHERE card_id = 45627);
