-- Each event counts a player's pulls in a row without its cards here, by
-- its key, so that two can run at once (the Demon Slayer event's beside the
-- Firefly Wedding's, from 2026-11-21); the Firefly event, begun before,
-- keeps its count in card_players.event_pulls until it ends.
CREATE TABLE IF NOT EXISTS card_event_pulls (
    user_id BIGINT NOT NULL,
    event TEXT NOT NULL,
    pulls INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, event)
);
