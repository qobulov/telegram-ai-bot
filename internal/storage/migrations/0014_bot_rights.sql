-- The admin rights Telegram last reported for the bot in each group, as a
-- JSON array of Bot API names such as "can_delete_messages", so that every
-- turn can tell the model what it may do without asking Telegram. NULL until
-- known; [] when the bot holds none.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS bot_rights jsonb;
