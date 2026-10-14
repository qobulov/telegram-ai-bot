-- Pictures drawn for each user, to apply the daily limit.

CREATE TABLE image_generations (
    id         bigserial   PRIMARY KEY,
    user_id    bigint      NOT NULL,
    chat_id    bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX image_generations_user_id_created_at_idx ON image_generations (user_id, created_at);
