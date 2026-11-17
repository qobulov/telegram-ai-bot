-- Crystals bought for Telegram Stars in the cards app: one row a payment,
-- named by Telegram's charge ID so that a payment reported twice adds its
-- crystals once; refunded_at marks one given back, its crystals taken.
CREATE TABLE crystal_purchases (
    charge_id   TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    stars       INTEGER NOT NULL,
    crystals    INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    refunded_at TIMESTAMPTZ
);
CREATE INDEX crystal_purchases_user ON crystal_purchases (user_id, created_at DESC);
