-- Moderation is taken at once now, without a question to confirm, so
-- nothing waits in pending_moderation any more. Its merged file is gone; a
-- database made before still has the table, empty.

DROP TABLE IF EXISTS pending_moderation;
