-- Telegram Business: the business accounts that connected the bot as their
-- chatbot, in Telegram's settings, the customer chats seen through them,
-- the answers given there, counted for the daily limit, and the checklists
-- sent there.
--
-- Only an owner's account is used. Anyone else's connection is recorded,
-- marked refused, and never answered through.

CREATE TABLE business_connections (
    -- Telegram's ID of the connection, which every message through it names.
    id                 text        PRIMARY KEY,
    -- The account's owner, who made the connection, and the bot's private
    -- chat with them.
    user_id            bigint      NOT NULL,
    user_chat_id       bigint      NOT NULL,
    first_name         text        NOT NULL DEFAULT '',
    username           text        NOT NULL DEFAULT '',
    -- What the owner let the bot do there: reply in chats that had incoming
    -- messages in the last day, and post stories.
    can_reply          boolean     NOT NULL DEFAULT false,
    can_manage_stories boolean     NOT NULL DEFAULT false,
    -- false once the owner disconnected the bot or turned the connection off.
    enabled            boolean     NOT NULL,
    -- true when the owner is none of the bot's owners.
    refused            boolean     NOT NULL DEFAULT false,
    -- Whether the bot answers customers there, as the owner's /business on
    -- or off sets for all their connections.
    auto_reply         boolean     NOT NULL DEFAULT false,
    connected_at       timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX business_connections_user_id_idx ON business_connections (user_id, updated_at);

CREATE TABLE business_chats (
    connection_id   text        NOT NULL REFERENCES business_connections (id) ON DELETE CASCADE,
    -- The private chat with the customer, whose ID is the customer's.
    chat_id         bigint      NOT NULL,
    first_name      text        NOT NULL DEFAULT '',
    username        text        NOT NULL DEFAULT '',
    last_message_at timestamptz NOT NULL DEFAULT now(),
    -- When the account's owner last wrote there themselves: the bot leaves
    -- the chat to them for a while after.
    owner_wrote_at  timestamptz,
    PRIMARY KEY (connection_id, chat_id)
);

CREATE INDEX business_chats_last_message_idx ON business_chats (connection_id, last_message_at);

-- One row per answer to a customer, for the daily limit per customer; the
-- daily cleanup deletes old ones.
CREATE TABLE business_replies (
    id            bigserial   PRIMARY KEY,
    connection_id text        NOT NULL,
    chat_id       bigint      NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX business_replies_chat_idx ON business_replies (connection_id, chat_id, created_at);

-- The checklists the bot sent into customer chats, so that the owner can
-- have one changed later: Telegram does not tell bots a message's content.
CREATE TABLE business_checklists (
    connection_id text        NOT NULL REFERENCES business_connections (id) ON DELETE CASCADE,
    chat_id       bigint      NOT NULL,
    message_id    bigint      NOT NULL,
    title         text        NOT NULL,
    -- The tasks' texts, in order: a JSON array of strings.
    tasks         jsonb       NOT NULL,
    sent_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, chat_id, message_id)
);
