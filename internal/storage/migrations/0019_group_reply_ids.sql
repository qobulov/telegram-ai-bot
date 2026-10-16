-- An answer reserved for a turn that then answered nothing, such as an
-- update Telegram delivered again, is given back, so each reservation needs
-- an ID to delete it by.

ALTER TABLE group_replies ADD COLUMN id bigserial PRIMARY KEY;
