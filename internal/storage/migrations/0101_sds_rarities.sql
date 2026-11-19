-- The Seven Deadly Sins' knights rise, as the owners asked: the Four Knights'
-- Lancelot and Tristan become Mythic; Zeldris, Estarossa (Mael) and the Four
-- Knights' Gawain and Percival become Legendary. Each takes its rarity's
-- numbers as the code gives them (CardOf), its name, anime and picture kept.
-- The players who hold one keep it, worth its new points, and have their
-- points counted again.
UPDATE card_catalog c SET rarity = v.rarity, attack = v.attack, health = v.health, value = v.value, updated_at = now()
FROM (VALUES
    (265209, 4, 4450, 17470, 4000),
    (249281, 4, 4120, 20090, 4000),
    (131559, 3, 2860, 10900, 2000),
    (126851, 3, 3400, 10530, 2000),
    (282753, 3, 3960, 9490, 2000),
    (206212, 3, 2700, 14840, 2000)
) AS v(id, rarity, attack, health, value)
WHERE c.id = v.id;

UPDATE card_players pl SET points = coalesce((
    SELECT sum(c.value) FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = pl.user_id
), 0)
WHERE pl.user_id IN (SELECT user_id FROM card_owned
    WHERE card_id IN (265209, 249281, 131559, 126851, 282753, 206212));
