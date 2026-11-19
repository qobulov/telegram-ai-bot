-- Escanor is Mythic, as the owners asked on 2026-11-19: 4000 points, a
-- Mythic's attack and life, and the portrait the owners sent; Lancelot and
-- Tristan Liones get the portraits the owners sent too. The pictures are
-- under new names, art being cached for a month, and the players who hold
-- Escanor have their points counted again.
UPDATE card_catalog SET rarity = 4, attack = 5300, health = 19800, value = 4000, updated_at = now()
WHERE id = 90202;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (90202, '/kartalar/art/90202b.jpg'),
    (265209, '/kartalar/art/265209b.jpg'),
    (249281, '/kartalar/art/249281b.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (90202, 265209, 249281);

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned WHERE card_id = 90202);
