-- Members' reports, kept so that the admins see them with /reports and
-- settle each with a button; and a warning's reason, a member's warnings
-- lapsing a month after the last.
CREATE TABLE reports (
    id            bigserial   PRIMARY KEY,
    chat_id       bigint      NOT NULL,
    reporter_id   bigint      NOT NULL,
    reporter_name text        NOT NULL DEFAULT '',
    target_id     bigint      NOT NULL,
    target_name   text        NOT NULL DEFAULT '',
    message_id    bigint      NOT NULL DEFAULT 0,
    quote         text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'open',
    handled_by    bigint,
    created_at    timestamptz NOT NULL DEFAULT now(),
    handled_at    timestamptz
);
CREATE INDEX reports_open ON reports (chat_id, created_at DESC) WHERE status = 'open';

ALTER TABLE member_warns ADD COLUMN reason text NOT NULL DEFAULT '';
