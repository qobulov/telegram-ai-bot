-- Each group's vote to ban a member, as Iris's: the vote /voteban opened
-- last, who voted for and against, kept as JSON by internal/service and
-- changed with the row locked, so that presses at once never count twice.
CREATE TABLE ban_votes (
    chat_id    BIGINT      PRIMARY KEY,
    state      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
