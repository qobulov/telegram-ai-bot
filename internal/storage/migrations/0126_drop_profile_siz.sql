-- The bot says "siz" to everyone, as the owners decided on 2026-11-21, so
-- a person's wish for it is no longer kept: the column was never read into
-- a turn, and all five profiles had it false.
ALTER TABLE user_profiles DROP COLUMN IF EXISTS siz;
