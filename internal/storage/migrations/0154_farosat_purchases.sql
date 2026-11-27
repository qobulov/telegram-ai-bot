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
