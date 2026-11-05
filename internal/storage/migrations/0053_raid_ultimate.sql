-- Dev jangi's skills are now Kuch, a buff kept in storm_until and
-- storm_ready, and Ulti, an ultimate charged by taps: ult_charge is the
-- taps towards it, ult_armed that the next blow is it. fire_left and
-- fire_ready are no longer read; they stay until the code before this has
-- stopped running, which reads them.

ALTER TABLE raid_heroes
    ADD COLUMN ult_charge integer NOT NULL DEFAULT 0,
    ADD COLUMN ult_armed  boolean NOT NULL DEFAULT false;
