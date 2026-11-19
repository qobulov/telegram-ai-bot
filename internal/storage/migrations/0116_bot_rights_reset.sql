-- Rights stored wrongly after the move to Supabase (2026-11-19): in pgx's
-- exec mode an empty []string went as '{}', which jsonb read as an object,
-- and "the bot's rights" could no longer be read. Those rows forget their
-- rights, so the bot asks Telegram again.
UPDATE groups SET bot_rights = NULL WHERE jsonb_typeof(bot_rights) = 'object';
UPDATE channels SET bot_rights = NULL WHERE jsonb_typeof(bot_rights) = 'object';
