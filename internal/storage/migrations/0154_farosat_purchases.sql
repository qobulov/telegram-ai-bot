-- Farosat bought with Telegram Stars in the Farosat Mini App, as the owners
-- asked on 2026-11-27: a gram a Star, added to the buyer's farosat in the
-- group they bought it for and counted in its ranking like any other. A
-- payment is kept once, by its charge, so that Telegram reporting it twice
-- adds nothing; a refund takes its grams back.
CREATE TABLE IF NOT EXISTS farosat_purchases (
    charge_id   text PRIMARY KEY,
    chat_id     bigint NOT NULL,
    user_id     bigint NOT NULL,
    stars       integer NOT NULL,
    grams       integer NOT NULL,
    paid_at     timestamp with time zone NOT NULL DEFAULT now(),
    refunded_at timestamp with time zone
);

-- Galaxies of the Farosat Mini App opened for Stars before a member's
-- farosat reaches them: a galaxy a payment, kept open for good unless the
-- payment is refunded.
CREATE TABLE IF NOT EXISTS farosat_galaxies (
    charge_id   text PRIMARY KEY,
    chat_id     bigint NOT NULL,
    user_id     bigint NOT NULL,
    galaxy_id   integer NOT NULL,
    stars       integer NOT NULL,
    paid_at     timestamp with time zone NOT NULL DEFAULT now(),
    refunded_at timestamp with time zone
);

CREATE INDEX IF NOT EXISTS farosat_galaxies_member ON farosat_galaxies (chat_id, user_id) WHERE refunded_at IS NULL;
