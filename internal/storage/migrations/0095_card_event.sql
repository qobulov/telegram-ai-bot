-- The Firefly Wedding event (until 2026-12-02): a player's event pulls in a
-- row since their last Satoko Kirigaya from it, the 50th sure to give her.
ALTER TABLE card_players ADD COLUMN event_pulls INT NOT NULL DEFAULT 0;
