-- Each voice message's transcript, kept by its file (the same in every
-- chat), so that a voice message is sent to the model once, not again each
-- time someone replies to it, as the owners asked to cut tokens on
-- 2026-11-21. words is the transcript cut to one line; '' is no speech.
-- The retention job deletes rows older than RETENTION_DAYS.
CREATE TABLE voice_transcripts (
    file_unique_id text PRIMARY KEY,
    words          text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX voice_transcripts_created_at_idx ON voice_transcripts (created_at);
ALTER TABLE voice_transcripts ENABLE ROW LEVEL SECURITY;
