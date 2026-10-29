-- Premium for private chats, bought with Telegram Stars: each payment, kept
-- by Telegram's charge ID so that a payment delivered twice counts once and
-- can be refunded, and how long each user's premium lasts. Payments are
-- money, so the daily cleanup never deletes them.

CREATE TABLE star_payments (
    charge_id   text        PRIMARY KEY,
    user_id     bigint      NOT NULL,
    first_name  text        NOT NULL DEFAULT '',
    username    text        NOT NULL DEFAULT '',
    stars       integer     NOT NULL,
    days        integer     NOT NULL,
    paid_at     timestamptz NOT NULL DEFAULT now(),
    refunded_at timestamptz
);

CREATE INDEX star_payments_user_id_paid_at_idx ON star_payments (user_id, paid_at);
CREATE INDEX star_payments_paid_at_idx ON star_payments (paid_at);

CREATE TABLE premium_users (
    user_id bigint      PRIMARY KEY,
    until   timestamptz NOT NULL
);
