-- Kartalar's auto opener: a player who turned it on with /avto has each card
-- opened for them as soon as it may be, and sent quietly to their private
-- chat in auto_language. It is off by default, and turned off when the bot
-- can no longer write to the player.
ALTER TABLE card_players
    ADD COLUMN auto_draw     boolean NOT NULL DEFAULT false,
    ADD COLUMN auto_language text    NOT NULL DEFAULT 'uz';

CREATE INDEX card_players_auto_idx ON card_players (last_draw_at) WHERE auto_draw;
