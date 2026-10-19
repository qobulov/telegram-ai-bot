-- A moderation action waits until the person who asked for it confirms it,
-- by replying to the question the bot posts. One per person and group.

CREATE TABLE pending_moderation (
    requester_id      bigint,
    chat_id           bigint,
    -- delete, mute, unmute, ban, unban, pin or unpin.
    action            text        NOT NULL,
    target_user_id    bigint      NOT NULL,
    target_message_id bigint      NOT NULL,
    target_name       text        NOT NULL,
    -- What the message acted on said, for the audit of a delete.
    target_text       text        NOT NULL DEFAULT '',
    -- How long a mute lasts.
    minutes           int         NOT NULL DEFAULT 0,
    reason            text        NOT NULL DEFAULT '',
    -- The update that asked.
    update_id         bigint      NOT NULL,
    -- The bot's question; only a reply to it confirms. Null until it is
    -- posted.
    prompt_message_id bigint,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (requester_id, chat_id)
);
