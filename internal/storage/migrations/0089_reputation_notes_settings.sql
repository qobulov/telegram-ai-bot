-- A group's own settings, one value per key: where its log goes, whether
-- its service messages are cleaned.
CREATE TABLE group_settings (
    chat_id BIGINT NOT NULL,
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,
    set_by  BIGINT NOT NULL,
    set_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, key)
);

-- Reputation: a member's thanks ("+", "rahmat") to another, one per giver
-- and receiver an hour, counted per group.
CREATE TABLE member_reputation (
    chat_id    BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    points     INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);
CREATE INDEX member_reputation_top ON member_reputation (chat_id, points DESC);

CREATE TABLE reputation_log (
    chat_id  BIGINT NOT NULL,
    from_id  BIGINT NOT NULL,
    to_id    BIGINT NOT NULL,
    given_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reputation_log_pair ON reputation_log (chat_id, from_id, to_id, given_at DESC);

-- Saved answers a group's admins keep: "#qoidalar" or "!donat" brings one
-- back, its text and perhaps a picture, sticker, GIF, video or file.
CREATE TABLE group_notes (
    chat_id    BIGINT NOT NULL,
    name       TEXT NOT NULL,
    text       TEXT NOT NULL DEFAULT '',
    media_kind TEXT NOT NULL DEFAULT '',
    file_id    TEXT NOT NULL DEFAULT '',
    saved_by   BIGINT NOT NULL,
    saved_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, name)
);
