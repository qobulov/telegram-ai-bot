-- The boss of the cards, as the owners asked on 2026-11-23: a foe that
-- never falls, fought with the arena team a few times a day; each fight is
-- a row of the damage dealt it, by the Tashkent week it was fought in.
CREATE TABLE boss_fights (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    week date NOT NULL,
    damage bigint NOT NULL,
    fought_at timestamp with time zone NOT NULL
);

CREATE INDEX boss_fights_week_idx ON boss_fights USING btree (week, user_id);
CREATE INDEX boss_fights_user_idx ON boss_fights USING btree (user_id, fought_at);

ALTER TABLE boss_fights ENABLE ROW LEVEL SECURITY;
