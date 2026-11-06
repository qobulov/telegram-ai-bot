-- Requests to join a group that wait for the requester to press the bot's
-- "I am human" button. A row is taken when the button is pressed, which
-- approves the request, or once expires_at has passed, which declines it.
CREATE TABLE join_requests (
    chat_id      bigint      NOT NULL,
    user_id      bigint      NOT NULL,
    -- The private chat the button was sent to.
    user_chat_id bigint      NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_id)
);

-- Finding the requests whose time is up.
CREATE INDEX join_requests_expires_at_idx ON join_requests (expires_at);

-- The owners' guard that sends the button, one group at a time: without it
-- the group's join requests are left to its admins.
ALTER TABLE group_rules DROP CONSTRAINT group_rules_kind_check;
ALTER TABLE group_rules ADD CONSTRAINT group_rules_kind_check
    CHECK (kind IN ('instruction', 'ignore', 'address', 'swear_guard', 'welcome', 'spam_guard', 'link_guard', 'image_guard',
                    'reply_limit', 'min_active', 'swear_punishment', 'picture_punishment', 'join_captcha'));
