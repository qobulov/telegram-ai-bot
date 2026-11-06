-- Dev jangi recalculated, as the owners asked, since no history of hits is
-- kept to do it exactly:
--
-- A member's total that no sword and crit could reach in their taps, nine
-- times a blow at their sword each tap (a critical blow under Kuch), is set
-- to what their taps do on average: a blow, a critical one as often as their
-- crit says. Owners' tests had left totals of up to 100 trillion.
UPDATE raid_members m
SET total = round(h.taps * 5 * power(1.4, h.sword - 1) * (1 + 2 * least(5 + 3 * h.crit, 100) / 100.0))::bigint
FROM raid_heroes h
WHERE h.user_id = m.user_id
  AND m.total > h.taps * 5 * power(1.4, h.sword - 1) * 9;

-- Each boss is set back to the level its active members, the last day's,
-- can kill: the one whose HP is 400 blows each of their median sword. The
-- bosses had grown past what most of them could hit, on the totals above
-- and on kills paid too much. Its members start that boss afresh.
WITH active AS (
    SELECT m.chat_id, count(*) AS members,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY 5 * power(1.4, h.sword - 1)) AS median_blow
    FROM raid_members m JOIN raid_heroes h ON h.user_id = m.user_id
    WHERE m.updated_at > now() - interval '1 day'
    GROUP BY m.chat_id
), target AS (
    SELECT b.chat_id, a.members,
           greatest(1, 1 + floor(ln(a.median_blow * 400 * a.members / (1500 * (0.6 + 0.4 * least(a.members, 30)))) / ln(1.32)))::int AS level
    FROM raid_bosses b JOIN active a ON a.chat_id = b.chat_id
), lowered AS (
    UPDATE raid_bosses b
    SET level = t.level,
        max_hp = round(1500 * power(1.32, t.level - 1) * (0.6 + 0.4 * least(t.members, 30)) * CASE WHEN t.level % 5 = 0 THEN 3 ELSE 1 END)::bigint,
        hp = round(1500 * power(1.32, t.level - 1) * (0.6 + 0.4 * least(t.members, 30)) * CASE WHEN t.level % 5 = 0 THEN 3 ELSE 1 END)::bigint,
        updated_at = now()
    FROM target t
    WHERE t.chat_id = b.chat_id AND t.level < b.level
    RETURNING b.chat_id, b.level
)
UPDATE raid_members m
SET level = l.level, damage = 0
FROM lowered l
WHERE m.chat_id = l.chat_id;

-- Patrick earned most of his coins while a kill paid 5% and 20% of a
-- sword's next level, a fourth of a level each: he keeps his sword, and his
-- coins are cut to a tenth of its next level, two chiefs' worth now.
UPDATE raid_heroes
SET coins = least(coins, round(0.10 * least(20 * power(1.6, sword - 1), 1e15))::bigint), updated_at = now()
WHERE user_id = 862862027;
