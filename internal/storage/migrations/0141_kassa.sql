-- Kassa, a cash book Mini App, as the owners asked on 2026-11-24: each
-- user's own money in (kirim) and out (chiqim), one row an entry with its
-- day, amount in so'm and a note; the balance is their sum.
CREATE TABLE kassa_entries (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    kind text NOT NULL CHECK (kind IN ('in', 'out')),
    amount bigint NOT NULL CHECK (amount > 0),
    note text DEFAULT '' NOT NULL,
    day date NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX kassa_entries_user_idx ON kassa_entries USING btree (user_id, kind, day DESC, id DESC);

ALTER TABLE kassa_entries ENABLE ROW LEVEL SECURITY;
