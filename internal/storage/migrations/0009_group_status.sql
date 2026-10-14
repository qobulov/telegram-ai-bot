-- What Telegram last said about the bot in each group: "member",
-- "administrator", "left" or "kicked". NULL until it says something.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS bot_status text;

-- Groups seen before groups existed, from what is still stored. Their
-- titles are filled in by the next message or membership change.
INSERT INTO groups (chat_id)
SELECT chat_id FROM group_messages
UNION SELECT chat_id FROM conversations WHERE chat_id < 0
UNION SELECT chat_id FROM chats WHERE chat_id < 0
ON CONFLICT (chat_id) DO NOTHING;
