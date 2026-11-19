-- The best loved Epic characters rise, as the owners asked on 2026-11-19:
-- Solo Leveling's Sung Jinwoo becomes Mythic, with a cut-out so that he
-- breathes, and thirteen more become Legendary (Reigen, Artoria, Askeladd,
-- Giyuu, Hachiman, Bocchi, Homura, Nanami, Ayanokouji, Saiki, Dazai,
-- Kageyama, Yato), each with its rarity's numbers as the code gives them
-- (CardOf); Sasuke gets the portrait the owners sent, under a new name as
-- art is cached for a month. The players who hold one have their points
-- counted again.
UPDATE card_catalog c SET rarity = v.rarity, attack = v.attack, health = v.health, value = v.value, updated_at = now()
FROM (VALUES
    (89334, 3, 3020, 12470, 2000),
    (497, 3, 2880, 12120, 2000),
    (13020, 3, 3490, 13170, 2000),
    (130050, 3, 3650, 10860, 2000),
    (67065, 3, 3170, 14310, 2000),
    (257562, 3, 3240, 13130, 2000),
    (38005, 3, 3610, 10540, 2000),
    (133704, 3, 3450, 9690, 2000),
    (123212, 3, 3750, 12970, 2000),
    (90107, 3, 3560, 10120, 2000),
    (89198, 3, 2570, 13470, 2000),
    (64771, 3, 3540, 12580, 2000),
    (84677, 3, 3950, 13010, 2000),
    (129928, 4, 4580, 18150, 4000)
) AS v(id, rarity, attack, health, value)
WHERE c.id = v.id;

INSERT INTO card_pictures (card_id, image_url) VALUES (13, '/kartalar/art/13b.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;
UPDATE card_catalog SET image_url = '/kartalar/art/13b.jpg', updated_at = now() WHERE id = 13;

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned
    WHERE card_id IN (89334, 497, 13020, 130050, 67065, 257562, 38005, 133704, 123212, 90107, 89198, 64771, 84677, 129928));
