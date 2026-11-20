-- The owners asked on 2026-11-20 for no reminders at all: the reminders
-- tool, the card reminders and the every-minute endpoint are gone, and so
-- is what they kept.
DROP TABLE IF EXISTS reminders;
ALTER TABLE card_players DROP COLUMN IF EXISTS remind_chat_id, DROP COLUMN IF EXISTS remind_language;
