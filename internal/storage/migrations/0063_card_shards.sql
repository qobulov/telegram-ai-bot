-- 💎 shards for Kartalar, made of the spare copies of cards: a card drawn
-- again gave only points, and points only a place on the table. A melted copy
-- is still held and still counted in the points, so that melting costs no
-- place: melted counts the copies of a card gone to shards, never more than
-- all but one. Shards draw a card out of turn and are what a duel is fought
-- for; card_duels keeps each duel's message, so that one press of its button
-- settles it once.
ALTER TABLE card_players ADD COLUMN shards INTEGER NOT NULL DEFAULT 0 CHECK (shards >= 0);
ALTER TABLE card_owned ADD COLUMN melted INTEGER NOT NULL DEFAULT 0 CHECK (melted >= 0 AND melted < count);

CREATE TABLE card_duels (
    chat_id    BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    winner_id  BIGINT,
    loser_id   BIGINT,
    stake      INTEGER NOT NULL DEFAULT 0,
    fought_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, message_id)
);
