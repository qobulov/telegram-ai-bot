-- A group's own announcement of who brought the most people in: an admin
-- turns it on with /invites_auto, and when each period ends (a week from
-- Monday, a half month from the 1st or the 16th, or a month) the bot posts
-- the top of it, pins the post and tags the group's owner. posted is the
-- period last posted, so that none is posted twice.
CREATE TABLE invite_awards (
    chat_id  BIGINT      PRIMARY KEY,
    period   TEXT        NOT NULL CHECK (period IN ('week', 'half', 'month')),
    top      INTEGER     NOT NULL CHECK (top BETWEEN 1 AND 20),
    language TEXT        NOT NULL DEFAULT 'uz',
    set_by   BIGINT      NOT NULL,
    set_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    posted   TEXT        NOT NULL DEFAULT ''
);
