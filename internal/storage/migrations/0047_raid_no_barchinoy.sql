-- Dev jangi has no Barchinoy any more: each hero gets back the coins they
-- paid for her levels, what raid.Cost asked for each, 80 × 1.7^level.
-- The helper, rain_until and rain_ready columns are no longer read; they
-- stay until the code before this has stopped running, which reads them.

UPDATE raid_heroes
SET coins = coins + (
        SELECT coalesce(sum(round(80 * power(1.7::numeric, l))), 0)::bigint
        FROM generate_series(0, helper - 1) AS l),
    helper = 0
WHERE helper > 0;
