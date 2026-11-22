-- Six Mythics for Black Clover, as the owners asked on 2026-11-22, ahead of
-- its event: Asta rises from Legendary, Yuno, Julius Novachrono, Mereoleona,
-- Yami and Noelle from below, with cards.CardOf's Mythic attack, life and 4000
-- points, and the pictures the owners sent (600x900), their cut-outs under
-- art/cut and plates under art/plate; each has its own effect, drawn by the
-- app (SPECIAL's fx): Asta's anti-magic, Yuno's wind, Julius's time,
-- Mereoleona's fire, Yami's darkness and Noelle's sea, her Valkyrie dress.
--
-- Like Demon Slayer's (migration 132), all six are first taken from
-- everyone who held them, so that they come anew as Mythics from the event:
-- the holders lose the points and the card from their count, the arena teams
-- with any are dropped and open trades for any are called off. This comes
-- first, while the cards keep their old value.
UPDATE card_players p SET points = greatest(p.points - s.value, 0), cards = greatest(p.cards - s.held, 0)
FROM (
    SELECT o.user_id, sum(c.value) AS value, count(*) AS held
    FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
    WHERE o.card_id IN (123285, 123284, 124435, 124522, 124440, 123283) AND o.count > 0
    GROUP BY o.user_id
) s
WHERE p.user_id = s.user_id;

DELETE FROM card_owned WHERE card_id IN (123285, 123284, 124435, 124522, 124440, 123283);

DELETE FROM arena_teams WHERE front && ARRAY[123285, 123284, 124435, 124522, 124440, 123283]::bigint[] OR back && ARRAY[123285, 123284, 124435, 124522, 124440, 123283]::bigint[];

UPDATE card_trades SET status = 'off', done_at = now()
WHERE status = 'open' AND (give_card IN (123285, 123284, 124435, 124522, 124440, 123283) OR take_card IN (123285, 123284, 124435, 124522, 124440, 123283));

UPDATE card_catalog SET rarity = 4, attack = 4210, health = 20770, value = 4000, updated_at = now() WHERE id = 123285;
UPDATE card_catalog SET rarity = 4, attack = 4080, health = 17930, value = 4000, updated_at = now() WHERE id = 123284;
UPDATE card_catalog SET rarity = 4, attack = 4190, health = 15400, value = 4000, updated_at = now() WHERE id = 124435;
UPDATE card_catalog SET rarity = 4, attack = 4950, health = 20780, value = 4000, updated_at = now() WHERE id = 124522;
UPDATE card_catalog SET rarity = 4, attack = 4210, health = 16310, value = 4000, updated_at = now() WHERE id = 124440;
UPDATE card_catalog SET rarity = 4, attack = 4910, health = 17880, value = 4000, updated_at = now() WHERE id = 123283;

INSERT INTO card_pictures (card_id, image_url) VALUES
    (123285, '/kartalar/art/123285m.jpg'),
    (123284, '/kartalar/art/123284m.jpg'),
    (124435, '/kartalar/art/124435m.jpg'),
    (124522, '/kartalar/art/124522m.jpg'),
    (124440, '/kartalar/art/124440m.jpg'),
    (123283, '/kartalar/art/123283m.jpg')
ON CONFLICT (card_id) DO UPDATE SET image_url = EXCLUDED.image_url;

UPDATE card_catalog c SET image_url = p.image_url, updated_at = now()
FROM card_pictures p WHERE p.card_id = c.id AND c.id IN (123285, 123284, 124435, 124522, 124440, 123283);
