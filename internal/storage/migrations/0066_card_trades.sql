-- Kartalar's cards passed between players: a gift (/sovga) gives a card at
-- once, a trade (/almash) is offered and done when the other player accepts.
-- A card moves with its points. Each row is one gift or trade; a player gives
-- at most a few cards a day, counted from the rows done: as from_id the card
-- they gave, as to_id of a trade the card they gave back.
CREATE TABLE card_trades (
    id         BIGSERIAL   PRIMARY KEY,
    chat_id    BIGINT      NOT NULL,
    from_id    BIGINT      NOT NULL,
    to_id      BIGINT      NOT NULL,
    give_card  BIGINT      NOT NULL REFERENCES card_catalog (id),
    -- take_card is what the other player gives back; NULL for a gift.
    take_card  BIGINT      REFERENCES card_catalog (id),
    status     TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'off')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at    TIMESTAMPTZ
);

CREATE INDEX card_trades_from_idx ON card_trades (from_id, done_at) WHERE status = 'done';
CREATE INDEX card_trades_to_idx ON card_trades (to_id, done_at) WHERE status = 'done' AND take_card IS NOT NULL;
