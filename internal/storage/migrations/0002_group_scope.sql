-- Conversations become per chat and user, so that a user's private chat and
-- their conversation in each group are stored separately.

ALTER TABLE conversations ADD COLUMN chat_id bigint;

-- Every existing conversation is a private chat, where the Telegram chat ID
-- equals the user ID.
UPDATE conversations SET chat_id = user_id;

ALTER TABLE conversations ALTER COLUMN chat_id SET NOT NULL;

ALTER TABLE conversations DROP CONSTRAINT conversations_user_id_key;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_chat_id_user_id_key UNIQUE (chat_id, user_id);

-- The old unique constraint also served the foreign key to users; keep an
-- index for that now that it is gone.
CREATE INDEX conversations_user_id_idx ON conversations (user_id);

ALTER TABLE messages
    -- The Telegram message this row corresponds to: the incoming message for
    -- a user row, the bot's sent message for its answer.
    ADD COLUMN telegram_message_id bigint,
    -- The Telegram message this one replies to, if any.
    ADD COLUMN reply_to_message_id bigint;
