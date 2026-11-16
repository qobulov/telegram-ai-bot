-- Moderators' ranks, 1 to 5, as Iris keeps them: who has which in each group,
-- who gave every change, and the rank each command needs where a group's
-- creator set one.
CREATE TABLE member_ranks (
    chat_id  bigint      NOT NULL,
    user_id  bigint      NOT NULL,
    rank     smallint    NOT NULL CHECK (rank BETWEEN 1 AND 5),
    given_by bigint      NOT NULL,
    given_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);

CREATE TABLE rank_log (
    id       bigserial PRIMARY KEY,
    chat_id  bigint      NOT NULL,
    user_id  bigint      NOT NULL,
    rank     smallint    NOT NULL,
    given_by bigint      NOT NULL,
    given_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX rank_log_chat ON rank_log (chat_id, given_at DESC);

CREATE TABLE command_ranks (
    chat_id bigint   NOT NULL,
    command text     NOT NULL,
    rank    smallint NOT NULL CHECK (rank BETWEEN 0 AND 6),
    PRIMARY KEY (chat_id, command)
);
