-- Daily quests, as the owners asked: each player's progress on each of the
-- day's quests, counted by Tashkent date, and when its tokens were claimed
-- (NULL until then). A row is made by the first action that counts toward
-- its quest; progress never passes the quest's target.
CREATE TABLE card_quests (
    user_id    BIGINT      NOT NULL,
    day        DATE        NOT NULL,
    quest      TEXT        NOT NULL,
    progress   INT         NOT NULL DEFAULT 0,
    claimed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, day, quest)
);
