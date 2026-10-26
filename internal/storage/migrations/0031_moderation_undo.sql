-- What puts back an action the bot took for an admin, so that "undo"
-- can: the group's or member's state before it, as JSON. NULL for an
-- action that cannot be undone, such as a deleted message. undone_at marks
-- one already put back.
ALTER TABLE moderation_actions ADD COLUMN undo jsonb;
ALTER TABLE moderation_actions ADD COLUMN undone_at timestamptz;

CREATE INDEX moderation_actions_chat_id_undoable_idx ON moderation_actions (chat_id, id)
    WHERE undo IS NOT NULL AND undone_at IS NULL;
