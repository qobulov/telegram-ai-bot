-- Dev jangi's sword now grows 1.5 times a level in price and not 1.6. A hero's
-- coins are set to the same part of their sword's next level as before, so that
-- coins saved at the old prices do not buy the strongest several levels at once
-- and widen the gap the new prices are to narrow.
UPDATE raid_heroes
SET coins = round(coins * power(1.5 / 1.6, sword - 1))::bigint;
