-- What a person asked the bot to remember about how to talk to them, in
-- every group and chat: the name to call them and whether they want "siz".
-- Nothing they said is kept, and they can clear it.

CREATE TABLE user_profiles (
    user_id    bigint      PRIMARY KEY,
    call_name  text        NOT NULL DEFAULT '',
    siz        boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
