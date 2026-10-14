-- Broadcasts post text only again: an owner's voice message is a request
-- to act on, not something to repost.
ALTER TABLE broadcasts
    DROP COLUMN from_chat_id,
    DROP COLUMN message_id;
