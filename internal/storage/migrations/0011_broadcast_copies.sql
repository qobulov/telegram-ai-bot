-- A broadcast can copy a message, such as a voice message, instead of
-- sending text. Then text is empty and these name the message.
ALTER TABLE broadcasts
    ADD COLUMN from_chat_id bigint,
    ADD COLUMN message_id   bigint;
