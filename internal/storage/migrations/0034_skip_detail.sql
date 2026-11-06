-- Why an answer failed, from the error: "error" alone left the reason to
-- the platform's logs, which were gone by the time anyone looked.
ALTER TABLE skipped_messages ADD COLUMN detail text NOT NULL DEFAULT '';
