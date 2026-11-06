-- Songs recognised for each user, to apply the daily limit: the recognition
-- service allows a few thousand a month.

CREATE TABLE song_recognitions (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL,
    chat_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX song_recognitions_user_id_created_at_idx ON song_recognitions (user_id, created_at);
