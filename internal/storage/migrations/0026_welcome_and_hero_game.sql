-- A group's welcome for new members, an owner's words, is one more kind of
-- standing rule.
ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome'));

-- The "guess the hero" game: one round per group at a time. The answer is
-- kept here, never given to the model, and checked in code.
CREATE TABLE hero_games (
    chat_id    bigint      PRIMARY KEY,
    hero       text        NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    ends_at    timestamptz NOT NULL,
    winner_id  bigint
);

-- Points won in each group.
CREATE TABLE hero_scores (
    chat_id    bigint      NOT NULL,
    user_id    bigint      NOT NULL,
    name       text        NOT NULL DEFAULT '',
    points     integer     NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);
