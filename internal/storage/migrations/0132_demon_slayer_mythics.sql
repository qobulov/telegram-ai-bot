-- Six Mythics for Demon Slayer, as the owners asked on 2026-11-21: Giyu and
-- Zenitsu rise from Legendary, Kokushibo, Yoriichi, Muzan and Akaza from
-- below, with cards.CardOf's Mythic attack, life and 4000 points, and the
-- pictures the owners sent (600x900, under new names as art is cached for a
-- month), their cut-outs under art/cut and plates under art/plate; each has
-- its own effect, drawn by the app (SPECIAL's fx).
--
-- Like Killua and Kurapika (migration 131), all six are first taken from
-- everyone who held them, so that they come anew as Mythics (the owners gave
-- Giyu's and Zenitsu's holders tokens for theirs): the holders lose the
-- points and the card from their count, the arena teams with any are
-- dropped (a team of cards not held cannot fight) and open trades for any
-- are called off. This comes first, while the cards keep their old value.
UPDATE card_players p SET points = greatest(p.points - s.value, 0), cards = greatest(p.cards - s.held, 0)
FROM (
    SELECT o.user_id, sum(c.value) AS value, count(*) AS held
    FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
    WHERE o.card_id IN (130050, 129131, 141918, 149220, 129132, 141694) AND o.count > 0
    GROUP BY o.user_id
) s
WHERE p.user_id = s.user_id;

DELETE FROM card_owned WHERE card_id IN (130050, 129131, 141918, 149220, 129132, 141694);

DELETE FROM arena_teams WHERE front && ARRAY[130050, 129131, 141918, 149220, 129132, 141694]::bigint[] OR back && ARRAY[130050, 129131, 141918, 149220, 129132, 141694]::bigint[];

UPDATE card_trades SET status = 'off', done_at = now()
WHERE status = 'open' AND (give_card IN (130050, 129131, 141918, 149220, 129132, 141694) OR take_card IN (130050, 129131, 141918, 149220, 129132, 141694));

UPDATE card_catalog SET rarity = 4, attack = 5930, health = 21320, value = 4000, updated_at = now() WHERE id = 130050;
UPDATE card_catalog SET rarity = 4, attack = 5830, health = 21420, value = 4000, updated_at = now() WHERE id = 129131;
UPDATE card_catalog SET rarity = 4, attack = 5480, health = 16340, value = 4000, updated_at = now() WHERE id = 141918;
UPDATE card_catalog SET rarity = 4, attack = 5440, health = 17910, value = 4000, updated_at = now() WHERE id = 149220;
UPDATE card_catalog SET rarity = 4, attack = 4100, health = 21160, value = 4000, updated_at = now() WHERE id = 129132;
UPDATE card_catalog SET rarity = 4, attack = 5030, health = 16810, value = 4000, updated_at = now() WHERE id = 141694;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (130050, '/kartalar/art/130050m.jpg'),
    (129131, '/kartalar/art/129131m.jpg'),
    (141918, '/kartalar/art/141918m.jpg'),
    (149220, '/kartalar/art/149220m.jpg'),
    (129132, '/kartalar/art/129132m.jpg'),
    (141694, '/kartalar/art/141694m.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url, updated_at = now()
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (130050, 129131, 141918, 149220, 129132, 141694);
