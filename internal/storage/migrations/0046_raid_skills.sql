-- Dev jangi 2.0a: each hero's skills, when each is ready again and how
-- long it lasts, the day's quests with what the hero did towards them and
-- which were claimed, and the streak of days they came back on.

ALTER TABLE raid_heroes
    ADD COLUMN storm_until   timestamptz,
    ADD COLUMN storm_ready   timestamptz,
    ADD COLUMN fire_left     integer NOT NULL DEFAULT 0,
    ADD COLUMN fire_ready    timestamptz,
    ADD COLUMN rain_until    timestamptz,
    ADD COLUMN rain_ready    timestamptz,
    -- quest_day is the day, in Uzbekistan, the quest counts are for.
    ADD COLUMN quest_day     date,
    ADD COLUMN quest_taps    integer NOT NULL DEFAULT 0,
    ADD COLUMN quest_kills   integer NOT NULL DEFAULT 0,
    ADD COLUMN quest_skills  integer NOT NULL DEFAULT 0,
    -- quest_claimed has a bit for each quest whose reward was taken.
    ADD COLUMN quest_claimed integer NOT NULL DEFAULT 0,
    ADD COLUMN streak        integer NOT NULL DEFAULT 0,
    ADD COLUMN streak_day    date;
