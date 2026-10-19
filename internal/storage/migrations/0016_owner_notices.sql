-- Messages the bot passed on from a group to its owners' private chats, at
-- most one per group a day. Counting them here, not in memory, makes the
-- limit hold on serverless hosting.

CREATE TABLE owner_notices (
    id           bigserial   PRIMARY KEY,
    chat_id      bigint      NOT NULL,
    requester_id bigint      NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX owner_notices_chat_id_created_at_idx ON owner_notices (chat_id, created_at);
