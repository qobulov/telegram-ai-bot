-- Box inventory and mailbox, as the owners asked on 2026-11-19. A player
-- holds unopened boxes (card_boxes) and opens them in the app; the owners
-- send boxes, tokens or crystals by mail (card_mail), which the player
-- claims in the app: the boxes go to the inventory, the rest to the wallet.
CREATE TABLE card_boxes (
    user_id BIGINT NOT NULL,
    box     TEXT   NOT NULL,
    count   INT    NOT NULL CHECK (count >= 0),
    PRIMARY KEY (user_id, box)
);

CREATE TABLE card_mail (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL,
    note       TEXT        NOT NULL DEFAULT '',
    box        TEXT        NOT NULL DEFAULT '',
    boxes      INT         NOT NULL DEFAULT 0 CHECK (boxes >= 0),
    tokens     INT         NOT NULL DEFAULT 0 CHECK (tokens >= 0),
    crystals   INT         NOT NULL DEFAULT 0 CHECK (crystals >= 0),
    sent_by    BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at TIMESTAMPTZ
);
CREATE INDEX card_mail_user_idx ON card_mail (user_id, created_at DESC);
