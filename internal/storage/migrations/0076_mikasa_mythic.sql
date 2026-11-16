-- Mikasa Ackerman is Mythic, the first of the women there, as the owners
-- chose: her numbers are a Mythic's, and her holders' points are counted
-- again.
UPDATE card_catalog SET rarity = 4, attack = 4590, health = 21570, value = 4000, updated_at = now()
WHERE id = 40881;

UPDATE card_players p
SET points = (SELECT coalesce(sum(c.value), 0)
              FROM card_owned o JOIN card_catalog c ON c.id = o.card_id WHERE o.user_id = p.user_id)
WHERE EXISTS (SELECT 1 FROM card_owned o WHERE o.user_id = p.user_id AND o.card_id = 40881);
