-- Killua and Kurapika are Mythic, as the owners asked on 2026-11-21: a
-- Mythic's attack, life and 4000 points (cards.CardOf), and their holders'
-- points counted again; they and Gojo get the pictures the owners sent, under
-- new names as art is cached for a month, with cut-outs under art/cut so that
-- they breathe. Killua's lightning is drawn by the app (SPECIAL).
UPDATE card_catalog SET rarity = 4, attack = 4550, health = 15040, value = 4000, updated_at = now() WHERE id = 27;
UPDATE card_catalog SET rarity = 4, attack = 4810, health = 18890, value = 4000, updated_at = now() WHERE id = 28;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (27, '/kartalar/art/27m.jpg'),
    (127691, '/kartalar/art/127691b.jpg'),
    (28, '/kartalar/art/28m.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url, updated_at = now()
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (27, 127691, 28);

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned WHERE card_id IN (27, 28));
