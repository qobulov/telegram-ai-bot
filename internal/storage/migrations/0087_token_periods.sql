-- Tokens come every 2 hours now, not two a day: a player's free_day, the
-- last time they were given tokens, was the day's midnight, which would give
-- everyone the periods since then at once. It starts at the present period,
-- the even hour in Tashkent, instead.
UPDATE card_players
SET free_day = (date_trunc('hour', now() AT TIME ZONE 'Asia/Tashkent')
    - make_interval(hours => extract(hour FROM now() AT TIME ZONE 'Asia/Tashkent')::int % 2)) AT TIME ZONE 'Asia/Tashkent'
WHERE free_day IS NOT NULL;
