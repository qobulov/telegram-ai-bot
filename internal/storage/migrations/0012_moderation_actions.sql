-- What the bot did in groups for their admins and its owners.

CREATE TABLE moderation_actions (
    id                bigserial   PRIMARY KEY,
    chat_id           bigint      NOT NULL,
    -- delete, mute, unmute, ban, unban, pin, unpin, or poll.
    action            text        NOT NULL,
    requester_id      bigint      NOT NULL,
    target_user_id    bigint,
    target_message_id bigint,
    -- What a deleted message said, at most 500 characters.
    target_text       text        NOT NULL DEFAULT '',
    -- How long a mute lasts.
    minutes           int,
    reason            text        NOT NULL DEFAULT '',
    -- The Telegram update that asked. An update delivered twice cannot
    -- record, and so take, the same action twice.
    update_id         bigint      NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (update_id, action)
);

CREATE INDEX moderation_actions_requester_id_created_at_idx ON moderation_actions (requester_id, created_at);
CREATE INDEX moderation_actions_chat_id_action_created_at_idx ON moderation_actions (chat_id, action, created_at);
