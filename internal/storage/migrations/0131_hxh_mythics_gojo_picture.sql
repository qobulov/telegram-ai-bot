-- Killua and Kurapika are Mythic, as the owners asked on 2026-11-21: a
-- Mythic's attack, life and 4000 points (cards.CardOf); they and Gojo get the
-- pictures the owners sent, under new names as art is cached for a month,
-- with cut-outs under art/cut so that they breathe. Killua's lightning is
-- drawn by the app (SPECIAL).
--
-- The owners also asked that both be taken from everyone who held them as
-- Legendaries, so that they come anew as Mythics: their holders lose the
-- Legendary points and the card from their count, the arena teams with
-- either are dropped (a team of cards not held cannot fight; its owner makes
-- another), and open trades for either are called off. This comes first,
-- while the cards still have their Legendary value.
UPDATE card_players p SET points = greatest(p.points - s.value, 0), cards = greatest(p.cards - s.held, 0)
FROM (
    SELECT o.user_id, sum(c.value) AS value, count(*) AS held
    FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
    WHERE o.card_id IN (27, 28) AND o.count > 0
    GROUP BY o.user_id
) s
WHERE p.user_id = s.user_id;

DELETE FROM card_owned WHERE card_id IN (27, 28);

DELETE FROM arena_teams WHERE front && ARRAY[27, 28]::bigint[] OR back && ARRAY[27, 28]::bigint[];

UPDATE card_trades SET status = 'off', done_at = now()
WHERE status = 'open' AND (give_card IN (27, 28) OR take_card IN (27, 28));

UPDATE card_catalog SET rarity = 4, attack = 4550, health = 15040, value = 4000, updated_at = now() WHERE id = 27;
UPDATE card_catalog SET rarity = 4, attack = 4810, health = 18890, value = 4000, updated_at = now() WHERE id = 28;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (27, '/kartalar/art/27m.jpg'),
    (127691, '/kartalar/art/127691b.jpg'),
    (28, '/kartalar/art/28m.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url, updated_at = now()
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (27, 127691, 28);
