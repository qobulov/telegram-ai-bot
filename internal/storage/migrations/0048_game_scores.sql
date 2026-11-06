-- Each player's scores in Tarozi, Kosmik ilon and O'tinchi, by game and by
-- chat (0 for a game sent inline), beside the bests Telegram keeps on each
-- game message: the best, the last, how many plays were scored and when.

CREATE TABLE game_scores (
    game      text        NOT NULL,
    chat_id   bigint      NOT NULL,
    user_id   bigint      NOT NULL,
    name      text        NOT NULL DEFAULT '',
    best      integer     NOT NULL,
    last      integer     NOT NULL,
    plays     integer     NOT NULL DEFAULT 1,
    best_at   timestamptz NOT NULL,
    played_at timestamptz NOT NULL,
    PRIMARY KEY (game, chat_id, user_id)
);

CREATE INDEX game_scores_best ON game_scores (game, best DESC);
