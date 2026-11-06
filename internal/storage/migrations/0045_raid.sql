-- Dev jangi, the bot's boss raid game: each chat has one boss its members
-- hit together, each member what they hit the boss at its level with and in
-- all, and each player their hero's coins and upgrades. A hit changes the
-- boss's row with it locked, so the one hit that kills a boss is the only
-- one to.

CREATE TABLE raid_bosses (
    chat_id    bigint      PRIMARY KEY,
    level      integer     NOT NULL DEFAULT 1,
    hp         bigint      NOT NULL,
    max_hp     bigint      NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE raid_members (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    name       text        NOT NULL DEFAULT '',
    -- damage is what they hit the boss of level with; total in all.
    level      integer     NOT NULL DEFAULT 0,
    damage     bigint      NOT NULL DEFAULT 0,
    total      bigint      NOT NULL DEFAULT 0,
    -- scored_at is when their total was last set as their Telegram score.
    scored_at  timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE INDEX raid_members_level_idx ON raid_members (chat_id, level, damage DESC);

CREATE TABLE raid_heroes (
    user_id    bigint      PRIMARY KEY,
    coins      bigint      NOT NULL DEFAULT 0,
    sword      integer     NOT NULL DEFAULT 1,
    crit       integer     NOT NULL DEFAULT 0,
    helper     integer     NOT NULL DEFAULT 0,
    -- seed and taps say which of the hero's taps are critical: the page
    -- knows the seed, so it shows each tap's damage as the server counts it.
    seed       bigint      NOT NULL,
    taps       bigint      NOT NULL DEFAULT 0,
    last_hit   timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
