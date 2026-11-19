-- Arena tickets, as the owners asked: instead of five attacks a day a
-- player holds up to eight tickets, one coming back each hour. tickets is
-- the count at tickets_at, which is when the next hour's counting started;
-- NULL tickets_at is a full stock. The day's attacks columns stay unread,
-- so a binary of before this migration keeps working until the deploy.
-- Each battle also keeps the defender's own rating change, for the battle
-- history; battles before it get the attacker's change negated, which it
-- was unless a rating stopped at 0.
ALTER TABLE arena_players
    ADD COLUMN tickets    INT NOT NULL DEFAULT 0,
    ADD COLUMN tickets_at TIMESTAMPTZ;

ALTER TABLE arena_battles ADD COLUMN defender_change INT NOT NULL DEFAULT 0;
UPDATE arena_battles SET defender_change = -rating_change;

CREATE INDEX arena_battles_attacker_idx ON arena_battles (attacker, id DESC);
CREATE INDEX arena_battles_defender_idx ON arena_battles (defender, id DESC);
