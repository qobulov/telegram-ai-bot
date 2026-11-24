-- Kassa holds several cash books a user, as the owners asked on 2026-11-24:
-- each book has a name and its own entries; the entries written before are
-- moved to a book named "Asosiy kassa" of their user.
CREATE TABLE kassa_books (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX kassa_books_user_idx ON kassa_books USING btree (user_id, id);

ALTER TABLE kassa_books ENABLE ROW LEVEL SECURITY;

INSERT INTO kassa_books (user_id, name)
SELECT DISTINCT user_id, 'Asosiy kassa' FROM kassa_entries;

ALTER TABLE kassa_entries ADD COLUMN book_id bigint REFERENCES kassa_books (id) ON DELETE CASCADE;

UPDATE kassa_entries e SET book_id = b.id FROM kassa_books b WHERE b.user_id = e.user_id;

ALTER TABLE kassa_entries ALTER COLUMN book_id SET NOT NULL;

DROP INDEX kassa_entries_user_idx;

CREATE INDEX kassa_entries_book_idx ON kassa_entries USING btree (book_id, kind, day DESC, id DESC);
