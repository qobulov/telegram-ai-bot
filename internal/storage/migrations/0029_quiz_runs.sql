-- Tests people send as files, read in code, and their runs: a part of a
-- test asked question by question as timed quizzes in a private chat.

-- One row per test, whoever sent it first: the same file sent by another
-- student is the same test, so that its runs are ranked together.
CREATE TABLE quiz_tests (
    id         bigserial   PRIMARY KEY,
    owner_id   bigint      NOT NULL,
    title      text        NOT NULL,
    -- [{"q": question, "o": [options], "c": index of the right one}]
    questions  jsonb       NOT NULL,
    -- Of the questions, not of the file: the same test sent as .doc and as
    -- .pdf is one.
    hash       text        NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE quiz_runs (
    id             bigserial   PRIMARY KEY,
    test_id        bigint      NOT NULL REFERENCES quiz_tests (id) ON DELETE CASCADE,
    user_id        bigint      NOT NULL,
    chat_id        bigint      NOT NULL,
    -- The part: questions first_question to first_question+question_count-1.
    first_question int         NOT NULL,
    question_count int         NOT NULL,
    -- How many of them were asked and answered or missed.
    position       int         NOT NULL DEFAULT 0,
    correct        int         NOT NULL DEFAULT 0,
    wrong          int         NOT NULL DEFAULT 0,
    skipped        int         NOT NULL DEFAULT 0,
    missed_in_row  int         NOT NULL DEFAULT 0,
    -- The quiz waiting for an answer, and the index of its right option.
    poll_id        text,
    answer         int,
    status         text        NOT NULL DEFAULT 'ready'
                               CHECK (status IN ('ready', 'running', 'paused', 'done', 'stopped')),
    started_at     timestamptz,
    finished_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX quiz_runs_poll_id_idx ON quiz_runs (poll_id) WHERE poll_id IS NOT NULL;
CREATE INDEX quiz_runs_user_id_status_idx ON quiz_runs (user_id, status);
CREATE INDEX quiz_runs_test_id_first_question_idx ON quiz_runs (test_id, first_question) WHERE status = 'done';
