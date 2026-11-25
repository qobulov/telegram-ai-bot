-- What every model request cost, as the owners asked on 2026-11-25: the
-- tokens the provider counted, the client it was for (chat, judge, router,
-- ...) and, for a turn, the chat and member, so that cost can be read per
-- day, chat or member instead of guessed from the logs.
CREATE TABLE model_usage (
    id               bigserial PRIMARY KEY,
    at               timestamptz NOT NULL DEFAULT now(),
    purpose          text NOT NULL,
    model            text NOT NULL,
    chat_id          bigint NOT NULL DEFAULT 0,
    user_id          bigint NOT NULL DEFAULT 0,
    prompt_tokens    integer NOT NULL,
    cached_tokens    integer NOT NULL,
    output_tokens    integer NOT NULL,
    reasoning_tokens integer NOT NULL
);

CREATE INDEX model_usage_at ON model_usage (at);
CREATE INDEX model_usage_chat_at ON model_usage (chat_id, at);
