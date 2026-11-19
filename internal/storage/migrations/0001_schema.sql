-- The schema as migrations 1 to 106 left it, merged into one file on
-- 2026-11-19 (made by running them on an empty database and dumping it):
-- every table, index and constraint, the rows those migrations put into an
-- empty database (21 cards and 138 card pictures), and row level security
-- on every table, so a database behind Supabase's Data API shows its anon
-- and authenticated roles nothing; the bot connects as the tables' owner,
-- which row level security does not stop. A database that ran the
-- migrations one by one is at version 106 and skips this file. The next
-- migration is 0107; a new table there needs its own
-- ALTER TABLE ... ENABLE ROW LEVEL SECURITY.

CREATE TABLE arena_battles (
    id bigint NOT NULL,
    attacker bigint NOT NULL,
    defender bigint NOT NULL,
    seed bigint NOT NULL,
    attacker_won boolean NOT NULL,
    rating_change integer NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL,
    defender_change integer DEFAULT 0 NOT NULL
);

CREATE SEQUENCE arena_battles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE arena_battles_id_seq OWNED BY arena_battles.id;

CREATE TABLE arena_players (
    user_id bigint NOT NULL,
    rating integer DEFAULT 1000 NOT NULL,
    wins integer DEFAULT 0 NOT NULL,
    losses integer DEFAULT 0 NOT NULL,
    attacks_day date,
    attacks integer DEFAULT 0 NOT NULL,
    tokens_day date,
    tokens_won integer DEFAULT 0 NOT NULL,
    tickets integer DEFAULT 0 NOT NULL,
    tickets_at timestamp with time zone
);

CREATE TABLE arena_teams (
    user_id bigint NOT NULL,
    front bigint[] NOT NULL,
    back bigint[] NOT NULL,
    power integer NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE arena_weeks (
    week date NOT NULL,
    players integer DEFAULT 0 NOT NULL,
    paid_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE ban_votes (
    chat_id bigint NOT NULL,
    state jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE bot_replies (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    bot_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE bot_replies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE bot_replies_id_seq OWNED BY bot_replies.id;

CREATE TABLE bot_stickers (
    file_unique_id text NOT NULL,
    file_id text NOT NULL,
    pack text NOT NULL,
    emoji text DEFAULT ''::text NOT NULL,
    image_file_id text DEFAULT ''::text NOT NULL,
    categories text[] DEFAULT '{}'::text[] NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    labelled boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    used_count integer DEFAULT 0 NOT NULL,
    last_used_at timestamp with time zone
);

CREATE TABLE broadcasts (
    id bigint NOT NULL,
    owner_id bigint NOT NULL,
    update_id bigint NOT NULL,
    text text NOT NULL,
    chat_ids bigint[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    copy_chat_id bigint,
    copy_message_id bigint
);

CREATE SEQUENCE broadcasts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE broadcasts_id_seq OWNED BY broadcasts.id;

CREATE TABLE business_chats (
    connection_id text NOT NULL,
    chat_id bigint NOT NULL,
    first_name text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    last_message_at timestamp with time zone DEFAULT now() NOT NULL,
    owner_wrote_at timestamp with time zone
);

CREATE TABLE business_checklists (
    connection_id text NOT NULL,
    chat_id bigint NOT NULL,
    message_id bigint NOT NULL,
    title text NOT NULL,
    tasks jsonb NOT NULL,
    sent_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE business_connections (
    id text NOT NULL,
    user_id bigint NOT NULL,
    user_chat_id bigint NOT NULL,
    first_name text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    can_reply boolean DEFAULT false NOT NULL,
    can_manage_stories boolean DEFAULT false NOT NULL,
    enabled boolean NOT NULL,
    refused boolean DEFAULT false NOT NULL,
    auto_reply boolean DEFAULT false NOT NULL,
    connected_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE business_replies (
    id bigint NOT NULL,
    connection_id text NOT NULL,
    chat_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE business_replies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE business_replies_id_seq OWNED BY business_replies.id;

CREATE TABLE card_anime_rewards (
    user_id bigint NOT NULL,
    anime text NOT NULL,
    tokens integer NOT NULL,
    claimed_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE card_boxes (
    user_id bigint NOT NULL,
    box text NOT NULL,
    count integer NOT NULL,
    CONSTRAINT card_boxes_count_check CHECK ((count >= 0))
);

CREATE TABLE card_catalog (
    id bigint NOT NULL,
    name text NOT NULL,
    anime text DEFAULT ''::text NOT NULL,
    image_url text DEFAULT ''::text NOT NULL,
    favourites integer DEFAULT 0 NOT NULL,
    rarity smallint NOT NULL,
    attack integer NOT NULL,
    health integer NOT NULL,
    value integer NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    collection text DEFAULT 'anime'::text NOT NULL,
    drawn integer DEFAULT 0 NOT NULL
);

CREATE TABLE card_duels (
    chat_id bigint NOT NULL,
    message_id bigint NOT NULL,
    winner_id bigint,
    loser_id bigint,
    stake integer DEFAULT 0 NOT NULL,
    fought_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE card_mail (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    box text DEFAULT ''::text NOT NULL,
    boxes integer DEFAULT 0 NOT NULL,
    tokens integer DEFAULT 0 NOT NULL,
    crystals integer DEFAULT 0 NOT NULL,
    sent_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    claimed_at timestamp with time zone,
    CONSTRAINT card_mail_boxes_check CHECK ((boxes >= 0)),
    CONSTRAINT card_mail_crystals_check CHECK ((crystals >= 0)),
    CONSTRAINT card_mail_tokens_check CHECK ((tokens >= 0))
);

CREATE SEQUENCE card_mail_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE card_mail_id_seq OWNED BY card_mail.id;

CREATE TABLE card_owned (
    user_id bigint NOT NULL,
    card_id bigint NOT NULL,
    count integer DEFAULT 1 NOT NULL,
    first_at timestamp with time zone DEFAULT now() NOT NULL,
    melted integer DEFAULT 0 NOT NULL,
    stars smallint DEFAULT 1 NOT NULL,
    CONSTRAINT card_owned_check CHECK (((melted >= 0) AND (melted < count))),
    CONSTRAINT card_owned_stars_check CHECK (((stars >= 1) AND (stars <= 5)))
);

CREATE TABLE card_pictures (
    card_id bigint NOT NULL,
    image_url text NOT NULL
);

CREATE TABLE card_players (
    user_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    points bigint DEFAULT 0 NOT NULL,
    cards integer DEFAULT 0 NOT NULL,
    draws integer DEFAULT 0 NOT NULL,
    last_draw_at timestamp with time zone,
    remind_chat_id bigint,
    remind_language text DEFAULT 'uz'::text NOT NULL,
    since_legendary integer DEFAULT 0 NOT NULL,
    since_mythic integer DEFAULT 0 NOT NULL,
    shards integer DEFAULT 0 NOT NULL,
    free_day timestamp with time zone,
    free_used integer DEFAULT 0 NOT NULL,
    tokens integer DEFAULT 0 NOT NULL,
    event_pulls integer DEFAULT 0 NOT NULL,
    CONSTRAINT card_players_shards_check CHECK ((shards >= 0)),
    CONSTRAINT card_players_tokens_check CHECK ((tokens >= 0))
);

CREATE TABLE card_quests (
    user_id bigint NOT NULL,
    day date NOT NULL,
    quest text NOT NULL,
    progress integer DEFAULT 0 NOT NULL,
    claimed_at timestamp with time zone
);

CREATE TABLE card_trades (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    from_id bigint NOT NULL,
    to_id bigint,
    give_card bigint NOT NULL,
    take_card bigint,
    status text DEFAULT 'open'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    done_at timestamp with time zone,
    CONSTRAINT card_trades_status_check CHECK ((status = ANY (ARRAY['open'::text, 'done'::text, 'off'::text])))
);

CREATE SEQUENCE card_trades_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE card_trades_id_seq OWNED BY card_trades.id;

CREATE TABLE channels (
    chat_id bigint NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    username text,
    bot_status text NOT NULL,
    bot_rights jsonb DEFAULT '[]'::jsonb NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE chats (
    chat_id bigint NOT NULL,
    muted boolean DEFAULT false NOT NULL,
    updated_by bigint,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE command_ranks (
    chat_id bigint NOT NULL,
    command text NOT NULL,
    rank smallint NOT NULL,
    CONSTRAINT command_ranks_rank_check CHECK (((rank >= 0) AND (rank <= 6)))
);

CREATE TABLE conversations (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    user_id bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE conversations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE conversations_id_seq OWNED BY conversations.id;

CREATE TABLE crystal_purchases (
    charge_id text NOT NULL,
    user_id bigint NOT NULL,
    stars integer NOT NULL,
    crystals integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    refunded_at timestamp with time zone
);

CREATE TABLE game_scores (
    game text NOT NULL,
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    best integer NOT NULL,
    last integer NOT NULL,
    plays integer DEFAULT 1 NOT NULL,
    best_at timestamp with time zone NOT NULL,
    played_at timestamp with time zone NOT NULL
);

CREATE TABLE group_boosts (
    chat_id bigint NOT NULL,
    boost_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE group_invites (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    inviter_id bigint NOT NULL,
    inviter_name text DEFAULT ''::text NOT NULL,
    via text NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL,
    left_at timestamp with time zone,
    CONSTRAINT group_invites_via_check CHECK ((via = ANY (ARRAY['add'::text, 'link'::text])))
);

CREATE TABLE group_members (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    first_name text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    is_bot boolean DEFAULT false NOT NULL,
    status text DEFAULT 'member'::text NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE group_messages (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    message_id bigint,
    author_id bigint,
    author_name text NOT NULL,
    text text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE group_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE group_messages_id_seq OWNED BY group_messages.id;

CREATE TABLE group_notes (
    chat_id bigint NOT NULL,
    name text NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    media_kind text DEFAULT ''::text NOT NULL,
    file_id text DEFAULT ''::text NOT NULL,
    saved_by bigint NOT NULL,
    saved_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE group_replies (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    id bigint NOT NULL
);

CREATE SEQUENCE group_replies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE group_replies_id_seq OWNED BY group_replies.id;

CREATE TABLE group_rules (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    kind text NOT NULL,
    user_id bigint,
    user_name text DEFAULT ''::text NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    set_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    CONSTRAINT group_rules_kind_check CHECK ((kind = ANY (ARRAY['instruction'::text, 'ignore'::text, 'address'::text, 'swear_guard'::text, 'welcome'::text, 'spam_guard'::text, 'link_guard'::text, 'image_guard'::text, 'reply_limit'::text, 'min_active'::text, 'swear_punishment'::text, 'picture_punishment'::text, 'join_captcha'::text, 'flood_guard'::text])))
);

CREATE SEQUENCE group_rules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE group_rules_id_seq OWNED BY group_rules.id;

CREATE TABLE group_settings (
    chat_id bigint NOT NULL,
    key text NOT NULL,
    value text NOT NULL,
    set_by bigint NOT NULL,
    set_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE groups (
    chat_id bigint NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    username text,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    bot_status text,
    bot_rights jsonb
);

CREATE TABLE image_generations (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    chat_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE image_generations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE image_generations_id_seq OWNED BY image_generations.id;

CREATE TABLE invite_awards (
    chat_id bigint NOT NULL,
    period text NOT NULL,
    top integer NOT NULL,
    language text DEFAULT 'uz'::text NOT NULL,
    set_by bigint NOT NULL,
    set_at timestamp with time zone DEFAULT now() NOT NULL,
    posted text DEFAULT ''::text NOT NULL,
    CONSTRAINT invite_awards_period_check CHECK ((period = ANY (ARRAY['week'::text, 'half'::text, 'month'::text]))),
    CONSTRAINT invite_awards_top_check CHECK (((top >= 1) AND (top <= 20)))
);

CREATE TABLE join_requests (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    user_chat_id bigint NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE mafia_games (
    chat_id bigint NOT NULL,
    state jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE member_links (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    link text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE member_ranks (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    rank smallint NOT NULL,
    given_by bigint NOT NULL,
    given_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT member_ranks_rank_check CHECK (((rank >= 1) AND (rank <= 5)))
);

CREATE TABLE member_reputation (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    points integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE member_warns (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    CONSTRAINT member_warns_count_check CHECK ((count >= 0))
);

CREATE TABLE messages (
    id bigint NOT NULL,
    conversation_id bigint NOT NULL,
    role text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    tool_calls jsonb,
    tool_call_id text,
    update_id bigint,
    telegram_message_id bigint,
    reply_to_message_id bigint,
    author_id bigint,
    author_name text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT messages_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text, 'tool'::text]))),
    CONSTRAINT messages_tool_call_id_check CHECK (((role = 'tool'::text) = (tool_call_id IS NOT NULL))),
    CONSTRAINT messages_tool_calls_check CHECK (((tool_calls IS NULL) OR (role = 'assistant'::text)))
);

CREATE SEQUENCE messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE messages_id_seq OWNED BY messages.id;

CREATE TABLE moderation_actions (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    action text NOT NULL,
    requester_id bigint NOT NULL,
    target_user_id bigint,
    target_message_id bigint,
    target_text text DEFAULT ''::text NOT NULL,
    minutes integer,
    reason text DEFAULT ''::text NOT NULL,
    update_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    undo jsonb,
    undone_at timestamp with time zone
);

CREATE SEQUENCE moderation_actions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE moderation_actions_id_seq OWNED BY moderation_actions.id;

CREATE TABLE own_sticker_sets (
    name text NOT NULL,
    owner_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE owner_notices (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    requester_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE owner_notices_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE owner_notices_id_seq OWNED BY owner_notices.id;

CREATE TABLE picture_verdicts (
    file_unique_id text NOT NULL,
    unsafe boolean NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    judge text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT picture_verdicts_judge_check CHECK ((judge = ANY (ARRAY['sightengine'::text, 'model'::text])))
);

CREATE TABLE premium_users (
    user_id bigint NOT NULL,
    until timestamp with time zone NOT NULL
);

CREATE TABLE private_messages (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    denied boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE private_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE private_messages_id_seq OWNED BY private_messages.id;

CREATE TABLE raid_bosses (
    chat_id bigint NOT NULL,
    level integer DEFAULT 1 NOT NULL,
    hp bigint NOT NULL,
    max_hp bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE raid_heroes (
    user_id bigint NOT NULL,
    coins bigint DEFAULT 0 NOT NULL,
    sword integer DEFAULT 1 NOT NULL,
    crit integer DEFAULT 0 NOT NULL,
    helper integer DEFAULT 0 NOT NULL,
    seed bigint NOT NULL,
    taps bigint DEFAULT 0 NOT NULL,
    last_hit timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    storm_until timestamp with time zone,
    storm_ready timestamp with time zone,
    fire_left integer DEFAULT 0 NOT NULL,
    fire_ready timestamp with time zone,
    rain_until timestamp with time zone,
    rain_ready timestamp with time zone,
    quest_day date,
    quest_taps integer DEFAULT 0 NOT NULL,
    quest_kills integer DEFAULT 0 NOT NULL,
    quest_skills integer DEFAULT 0 NOT NULL,
    quest_claimed integer DEFAULT 0 NOT NULL,
    streak integer DEFAULT 0 NOT NULL,
    streak_day date,
    ult_charge integer DEFAULT 0 NOT NULL,
    ult_armed boolean DEFAULT false NOT NULL
);

CREATE TABLE raid_members (
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    level integer DEFAULT 0 NOT NULL,
    damage bigint DEFAULT 0 NOT NULL,
    total bigint DEFAULT 0 NOT NULL,
    scored_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE rank_log (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    rank smallint NOT NULL,
    given_by bigint NOT NULL,
    given_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE rank_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE rank_log_id_seq OWNED BY rank_log.id;

CREATE TABLE reminders (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    user_name text DEFAULT ''::text NOT NULL,
    message_id bigint DEFAULT 0 NOT NULL,
    kind text DEFAULT 'text'::text NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    language text DEFAULT 'uz'::text NOT NULL,
    due_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    claimed_at timestamp with time zone,
    sent_at timestamp with time zone,
    outcome text DEFAULT ''::text NOT NULL,
    CONSTRAINT reminders_kind_check CHECK ((kind = ANY (ARRAY['text'::text, 'card'::text])))
);

CREATE SEQUENCE reminders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE reminders_id_seq OWNED BY reminders.id;

CREATE TABLE reports (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    reporter_id bigint NOT NULL,
    reporter_name text DEFAULT ''::text NOT NULL,
    target_id bigint NOT NULL,
    target_name text DEFAULT ''::text NOT NULL,
    message_id bigint DEFAULT 0 NOT NULL,
    quote text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    handled_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    handled_at timestamp with time zone
);

CREATE SEQUENCE reports_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE reports_id_seq OWNED BY reports.id;

CREATE TABLE reputation_log (
    chat_id bigint NOT NULL,
    from_id bigint NOT NULL,
    to_id bigint NOT NULL,
    given_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE settings (
    chat_id bigint DEFAULT 0 NOT NULL,
    name text NOT NULL,
    value text NOT NULL,
    set_by bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE skipped_messages (
    id bigint NOT NULL,
    chat_id bigint NOT NULL,
    user_id bigint NOT NULL,
    message_id bigint NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    detail text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE skipped_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE skipped_messages_id_seq OWNED BY skipped_messages.id;

CREATE TABLE song_recognitions (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    chat_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE song_recognitions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE song_recognitions_id_seq OWNED BY song_recognitions.id;

CREATE TABLE star_payments (
    charge_id text NOT NULL,
    user_id bigint NOT NULL,
    first_name text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    stars integer NOT NULL,
    days integer NOT NULL,
    paid_at timestamp with time zone DEFAULT now() NOT NULL,
    refunded_at timestamp with time zone
);

CREATE TABLE sticker_sends (
    chat_id bigint NOT NULL,
    sent_at timestamp with time zone NOT NULL
);

CREATE TABLE sticker_uploads (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    chat_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE sticker_uploads_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE sticker_uploads_id_seq OWNED BY sticker_uploads.id;

CREATE TABLE team_lobbies (
    chat_id bigint NOT NULL,
    state jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE user_profiles (
    user_id bigint NOT NULL,
    call_name text DEFAULT ''::text NOT NULL,
    siz boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE users (
    telegram_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY arena_battles ALTER COLUMN id SET DEFAULT nextval('arena_battles_id_seq'::regclass);

ALTER TABLE ONLY bot_replies ALTER COLUMN id SET DEFAULT nextval('bot_replies_id_seq'::regclass);

ALTER TABLE ONLY broadcasts ALTER COLUMN id SET DEFAULT nextval('broadcasts_id_seq'::regclass);

ALTER TABLE ONLY business_replies ALTER COLUMN id SET DEFAULT nextval('business_replies_id_seq'::regclass);

ALTER TABLE ONLY card_mail ALTER COLUMN id SET DEFAULT nextval('card_mail_id_seq'::regclass);

ALTER TABLE ONLY card_trades ALTER COLUMN id SET DEFAULT nextval('card_trades_id_seq'::regclass);

ALTER TABLE ONLY conversations ALTER COLUMN id SET DEFAULT nextval('conversations_id_seq'::regclass);

ALTER TABLE ONLY group_messages ALTER COLUMN id SET DEFAULT nextval('group_messages_id_seq'::regclass);

ALTER TABLE ONLY group_replies ALTER COLUMN id SET DEFAULT nextval('group_replies_id_seq'::regclass);

ALTER TABLE ONLY group_rules ALTER COLUMN id SET DEFAULT nextval('group_rules_id_seq'::regclass);

ALTER TABLE ONLY image_generations ALTER COLUMN id SET DEFAULT nextval('image_generations_id_seq'::regclass);

ALTER TABLE ONLY messages ALTER COLUMN id SET DEFAULT nextval('messages_id_seq'::regclass);

ALTER TABLE ONLY moderation_actions ALTER COLUMN id SET DEFAULT nextval('moderation_actions_id_seq'::regclass);

ALTER TABLE ONLY owner_notices ALTER COLUMN id SET DEFAULT nextval('owner_notices_id_seq'::regclass);

ALTER TABLE ONLY private_messages ALTER COLUMN id SET DEFAULT nextval('private_messages_id_seq'::regclass);

ALTER TABLE ONLY rank_log ALTER COLUMN id SET DEFAULT nextval('rank_log_id_seq'::regclass);

ALTER TABLE ONLY reminders ALTER COLUMN id SET DEFAULT nextval('reminders_id_seq'::regclass);

ALTER TABLE ONLY reports ALTER COLUMN id SET DEFAULT nextval('reports_id_seq'::regclass);

ALTER TABLE ONLY skipped_messages ALTER COLUMN id SET DEFAULT nextval('skipped_messages_id_seq'::regclass);

ALTER TABLE ONLY song_recognitions ALTER COLUMN id SET DEFAULT nextval('song_recognitions_id_seq'::regclass);

ALTER TABLE ONLY sticker_uploads ALTER COLUMN id SET DEFAULT nextval('sticker_uploads_id_seq'::regclass);

ALTER TABLE ONLY arena_battles
    ADD CONSTRAINT arena_battles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY arena_players
    ADD CONSTRAINT arena_players_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY arena_teams
    ADD CONSTRAINT arena_teams_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY arena_weeks
    ADD CONSTRAINT arena_weeks_pkey PRIMARY KEY (week);

ALTER TABLE ONLY ban_votes
    ADD CONSTRAINT ban_votes_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY bot_replies
    ADD CONSTRAINT bot_replies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY bot_stickers
    ADD CONSTRAINT bot_stickers_pkey PRIMARY KEY (file_unique_id);

ALTER TABLE ONLY broadcasts
    ADD CONSTRAINT broadcasts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY business_chats
    ADD CONSTRAINT business_chats_pkey PRIMARY KEY (connection_id, chat_id);

ALTER TABLE ONLY business_checklists
    ADD CONSTRAINT business_checklists_pkey PRIMARY KEY (connection_id, chat_id, message_id);

ALTER TABLE ONLY business_connections
    ADD CONSTRAINT business_connections_pkey PRIMARY KEY (id);

ALTER TABLE ONLY business_replies
    ADD CONSTRAINT business_replies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY card_anime_rewards
    ADD CONSTRAINT card_anime_rewards_pkey PRIMARY KEY (user_id, anime);

ALTER TABLE ONLY card_boxes
    ADD CONSTRAINT card_boxes_pkey PRIMARY KEY (user_id, box);

ALTER TABLE ONLY card_catalog
    ADD CONSTRAINT card_catalog_pkey PRIMARY KEY (id);

ALTER TABLE ONLY card_duels
    ADD CONSTRAINT card_duels_pkey PRIMARY KEY (chat_id, message_id);

ALTER TABLE ONLY card_mail
    ADD CONSTRAINT card_mail_pkey PRIMARY KEY (id);

ALTER TABLE ONLY card_owned
    ADD CONSTRAINT card_owned_pkey PRIMARY KEY (user_id, card_id);

ALTER TABLE ONLY card_pictures
    ADD CONSTRAINT card_pictures_pkey PRIMARY KEY (card_id);

ALTER TABLE ONLY card_players
    ADD CONSTRAINT card_players_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY card_quests
    ADD CONSTRAINT card_quests_pkey PRIMARY KEY (user_id, day, quest);

ALTER TABLE ONLY card_trades
    ADD CONSTRAINT card_trades_pkey PRIMARY KEY (id);

ALTER TABLE ONLY channels
    ADD CONSTRAINT channels_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY chats
    ADD CONSTRAINT chats_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY command_ranks
    ADD CONSTRAINT command_ranks_pkey PRIMARY KEY (chat_id, command);

ALTER TABLE ONLY conversations
    ADD CONSTRAINT conversations_chat_id_user_id_key UNIQUE (chat_id, user_id);

ALTER TABLE ONLY conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY crystal_purchases
    ADD CONSTRAINT crystal_purchases_pkey PRIMARY KEY (charge_id);

ALTER TABLE ONLY game_scores
    ADD CONSTRAINT game_scores_pkey PRIMARY KEY (game, chat_id, user_id);

ALTER TABLE ONLY group_boosts
    ADD CONSTRAINT group_boosts_pkey PRIMARY KEY (chat_id, boost_id);

ALTER TABLE ONLY group_invites
    ADD CONSTRAINT group_invites_pkey PRIMARY KEY (chat_id, user_id, inviter_id);

ALTER TABLE ONLY group_members
    ADD CONSTRAINT group_members_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY group_messages
    ADD CONSTRAINT group_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY group_notes
    ADD CONSTRAINT group_notes_pkey PRIMARY KEY (chat_id, name);

ALTER TABLE ONLY group_replies
    ADD CONSTRAINT group_replies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY group_rules
    ADD CONSTRAINT group_rules_pkey PRIMARY KEY (id);

ALTER TABLE ONLY group_settings
    ADD CONSTRAINT group_settings_pkey PRIMARY KEY (chat_id, key);

ALTER TABLE ONLY groups
    ADD CONSTRAINT groups_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY image_generations
    ADD CONSTRAINT image_generations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY invite_awards
    ADD CONSTRAINT invite_awards_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY join_requests
    ADD CONSTRAINT join_requests_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY mafia_games
    ADD CONSTRAINT mafia_games_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY member_links
    ADD CONSTRAINT member_links_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY member_ranks
    ADD CONSTRAINT member_ranks_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY member_reputation
    ADD CONSTRAINT member_reputation_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY member_warns
    ADD CONSTRAINT member_warns_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY moderation_actions
    ADD CONSTRAINT moderation_actions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY moderation_actions
    ADD CONSTRAINT moderation_actions_update_id_action_key UNIQUE (update_id, action);

ALTER TABLE ONLY own_sticker_sets
    ADD CONSTRAINT own_sticker_sets_pkey PRIMARY KEY (name);

ALTER TABLE ONLY owner_notices
    ADD CONSTRAINT owner_notices_pkey PRIMARY KEY (id);

ALTER TABLE ONLY picture_verdicts
    ADD CONSTRAINT picture_verdicts_pkey PRIMARY KEY (file_unique_id);

ALTER TABLE ONLY premium_users
    ADD CONSTRAINT premium_users_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY private_messages
    ADD CONSTRAINT private_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY raid_bosses
    ADD CONSTRAINT raid_bosses_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY raid_heroes
    ADD CONSTRAINT raid_heroes_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY raid_members
    ADD CONSTRAINT raid_members_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY rank_log
    ADD CONSTRAINT rank_log_pkey PRIMARY KEY (id);

ALTER TABLE ONLY reminders
    ADD CONSTRAINT reminders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY reports
    ADD CONSTRAINT reports_pkey PRIMARY KEY (id);

ALTER TABLE ONLY settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (chat_id, name);

ALTER TABLE ONLY skipped_messages
    ADD CONSTRAINT skipped_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY song_recognitions
    ADD CONSTRAINT song_recognitions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY star_payments
    ADD CONSTRAINT star_payments_pkey PRIMARY KEY (charge_id);

ALTER TABLE ONLY sticker_sends
    ADD CONSTRAINT sticker_sends_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY sticker_uploads
    ADD CONSTRAINT sticker_uploads_pkey PRIMARY KEY (id);

ALTER TABLE ONLY team_lobbies
    ADD CONSTRAINT team_lobbies_pkey PRIMARY KEY (chat_id);

ALTER TABLE ONLY user_profiles
    ADD CONSTRAINT user_profiles_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (telegram_id);

CREATE INDEX arena_battles_attacker_idx ON arena_battles USING btree (attacker, id DESC);

CREATE INDEX arena_battles_defender_idx ON arena_battles USING btree (defender, id DESC);

CREATE INDEX arena_players_rating_idx ON arena_players USING btree (rating);

CREATE INDEX bot_replies_chat_id_created_at_idx ON bot_replies USING btree (chat_id, created_at);

CREATE INDEX bot_stickers_categories_idx ON bot_stickers USING gin (categories);

CREATE INDEX broadcasts_owner_id_idx ON broadcasts USING btree (owner_id, id);

CREATE INDEX business_chats_last_message_idx ON business_chats USING btree (connection_id, last_message_at);

CREATE INDEX business_connections_user_id_idx ON business_connections USING btree (user_id, updated_at);

CREATE INDEX business_replies_chat_idx ON business_replies USING btree (connection_id, chat_id, created_at);

CREATE INDEX card_catalog_rarity_idx ON card_catalog USING btree (rarity);

CREATE INDEX card_mail_user_idx ON card_mail USING btree (user_id, created_at DESC);

CREATE INDEX card_players_points_idx ON card_players USING btree (points DESC);

CREATE INDEX card_trades_from_idx ON card_trades USING btree (from_id, done_at) WHERE (status = 'done'::text);

CREATE INDEX card_trades_market_idx ON card_trades USING btree (created_at DESC) WHERE ((status = 'open'::text) AND (to_id IS NULL));

CREATE INDEX card_trades_to_idx ON card_trades USING btree (to_id, done_at) WHERE ((status = 'done'::text) AND (take_card IS NOT NULL));

CREATE UNIQUE INDEX conversations_group_key ON conversations USING btree (chat_id) WHERE (user_id IS NULL);

CREATE INDEX conversations_updated_at_idx ON conversations USING btree (updated_at);

CREATE INDEX conversations_user_id_idx ON conversations USING btree (user_id);

CREATE INDEX crystal_purchases_user ON crystal_purchases USING btree (user_id, created_at DESC);

CREATE INDEX game_scores_best ON game_scores USING btree (game, best DESC);

CREATE INDEX group_invites_chat_idx ON group_invites USING btree (chat_id, joined_at);

CREATE INDEX group_invites_user_idx ON group_invites USING btree (chat_id, user_id);

CREATE INDEX group_members_chat_id_last_seen_at_idx ON group_members USING btree (chat_id, last_seen_at DESC);

CREATE INDEX group_messages_chat_id_created_at_idx ON group_messages USING btree (chat_id, created_at);

CREATE INDEX group_messages_created_at_idx ON group_messages USING btree (created_at);

CREATE INDEX group_replies_chat_id_created_at_idx ON group_replies USING btree (chat_id, created_at);

CREATE INDEX group_rules_chat_id_idx ON group_rules USING btree (chat_id);

CREATE INDEX image_generations_user_id_created_at_idx ON image_generations USING btree (user_id, created_at);

CREATE INDEX join_requests_expires_at_idx ON join_requests USING btree (expires_at);

CREATE INDEX member_reputation_top ON member_reputation USING btree (chat_id, points DESC);

CREATE INDEX messages_conversation_id_id_idx ON messages USING btree (conversation_id, id);

CREATE UNIQUE INDEX messages_update_id_key ON messages USING btree (update_id) WHERE (update_id IS NOT NULL);

CREATE INDEX moderation_actions_chat_id_action_created_at_idx ON moderation_actions USING btree (chat_id, action, created_at);

CREATE INDEX moderation_actions_chat_id_undoable_idx ON moderation_actions USING btree (chat_id, id) WHERE ((undo IS NOT NULL) AND (undone_at IS NULL));

CREATE INDEX moderation_actions_requester_id_created_at_idx ON moderation_actions USING btree (requester_id, created_at);

CREATE INDEX owner_notices_chat_id_created_at_idx ON owner_notices USING btree (chat_id, created_at);

CREATE INDEX private_messages_user_id_created_at_idx ON private_messages USING btree (user_id, created_at);

CREATE INDEX raid_members_level_idx ON raid_members USING btree (chat_id, level, damage DESC);

CREATE INDEX rank_log_chat ON rank_log USING btree (chat_id, given_at DESC);

CREATE INDEX reminders_due_idx ON reminders USING btree (due_at) WHERE (claimed_at IS NULL);

CREATE INDEX reminders_finished_idx ON reminders USING btree (claimed_at) WHERE (claimed_at IS NOT NULL);

CREATE UNIQUE INDEX reminders_one_card_idx ON reminders USING btree (user_id) WHERE ((kind = 'card'::text) AND (claimed_at IS NULL));

CREATE INDEX reminders_waiting_idx ON reminders USING btree (user_id, chat_id) WHERE (claimed_at IS NULL);

CREATE INDEX reports_open ON reports USING btree (chat_id, created_at DESC) WHERE (status = 'open'::text);

CREATE INDEX reputation_log_pair ON reputation_log USING btree (chat_id, from_id, to_id, given_at DESC);

CREATE INDEX skipped_messages_created_at_idx ON skipped_messages USING btree (created_at);

CREATE INDEX song_recognitions_user_id_created_at_idx ON song_recognitions USING btree (user_id, created_at);

CREATE INDEX star_payments_paid_at_idx ON star_payments USING btree (paid_at);

CREATE INDEX star_payments_user_id_paid_at_idx ON star_payments USING btree (user_id, paid_at);

CREATE INDEX sticker_uploads_user_id_created_at_idx ON sticker_uploads USING btree (user_id, created_at);

ALTER TABLE ONLY business_chats
    ADD CONSTRAINT business_chats_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES business_connections(id) ON DELETE CASCADE;

ALTER TABLE ONLY business_checklists
    ADD CONSTRAINT business_checklists_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES business_connections(id) ON DELETE CASCADE;

ALTER TABLE ONLY card_owned
    ADD CONSTRAINT card_owned_card_id_fkey FOREIGN KEY (card_id) REFERENCES card_catalog(id);

ALTER TABLE ONLY card_trades
    ADD CONSTRAINT card_trades_give_card_fkey FOREIGN KEY (give_card) REFERENCES card_catalog(id);

ALTER TABLE ONLY card_trades
    ADD CONSTRAINT card_trades_take_card_fkey FOREIGN KEY (take_card) REFERENCES card_catalog(id);

ALTER TABLE ONLY conversations
    ADD CONSTRAINT conversations_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(telegram_id) ON DELETE CASCADE;

ALTER TABLE ONLY messages
    ADD CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE;

INSERT INTO card_catalog VALUES
	(122210, 'Arthur Pendragon', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b122210-l6g1EMCf5NdE.png', 558, 2, 2010, 7440, 400, '2026-11-19 22:34:18.221099+09', 'anime', 0),
	(247947, 'Nasiens', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247947-7KpURs8nZNbT.png', 55, 1, 1060, 3430, 200, '2026-11-19 22:34:18.221099+09', 'anime', 0),
	(247949, 'Anghalhad', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247949-8O2zw4YYJJwm.png', 37, 1, 1500, 3930, 200, '2026-11-19 22:34:18.221099+09', 'anime', 0),
	(247948, 'Donny', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b247948-j95hDCFSuAIm.png', 20, 1, 1160, 4110, 200, '2026-11-19 22:34:18.221099+09', 'anime', 0),
	(298762, 'Shinpei Gotou', 'Firefly Wedding', 'https://s4.anilist.co/file/anilistcdn/character/large/b298762-GJdjyPmhS8Ln.png', 575, 2, 1800, 8040, 400, '2026-11-19 22:34:18.222016+09', 'anime', 0),
	(137079, 'Power', 'Chainsaw Man', 'https://s4.anilist.co/file/anilistcdn/character/large/b137079-6yLEUYR3bmpr.png', 20439, 3, 3210, 13360, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(120649, 'Kaguya Shinomiya', 'Kaguya-sama: Love is War', 'https://s4.anilist.co/file/anilistcdn/character/large/b120649-NPaWaIpWy60E.png', 20305, 3, 3760, 13430, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(133676, 'Marin Kitagawa', 'My Dress-Up Darling', 'https://s4.anilist.co/file/anilistcdn/character/large/b133676-kV2czE3C8Qls.png', 17325, 3, 3630, 11050, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(89361, 'Megumin', 'KONOSUBA -God''s blessing on this wonderful world!', 'https://s4.anilist.co/file/anilistcdn/character/large/b89361-tq8PQQ4MmF0M.png', 16532, 3, 3480, 14050, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(61, 'Robin Nico', 'ONE PIECE', 'https://s4.anilist.co/file/anilistcdn/character/large/b61-ywXUyyocEEqt.png', 14890, 3, 2910, 14280, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(94, 'Asuka Langley Souryuu', 'Neon Genesis Evangelion', 'https://s4.anilist.co/file/anilistcdn/character/large/b94-d631a3Z2KPvd.png', 14757, 3, 2660, 11760, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(22037, 'Hitagi Senjougahara', 'Bakemonogatari', 'https://s4.anilist.co/file/anilistcdn/character/large/b22037-sY7GWSKYr2Nl.jpg', 14717, 3, 2960, 12010, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(138102, 'Yor Forger', 'SPY x FAMILY', 'https://s4.anilist.co/file/anilistcdn/character/large/b138102-ZOAu9jI2d5ke.png', 14391, 3, 3870, 10000, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(133700, 'Nobara Kugisaki', 'JUJUTSU KAISEN', 'https://s4.anilist.co/file/anilistcdn/character/large/b133700-f6sOO3TcgLV6.png', 13559, 3, 2590, 14140, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(138100, 'Anya Forger', 'SPY x FAMILY', 'https://s4.anilist.co/file/anilistcdn/character/large/b138100-4Li0tWRCa5bQ.png', 13230, 3, 3130, 14430, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(723, 'Nami', 'ONE PIECE', 'https://s4.anilist.co/file/anilistcdn/character/large/b723-vp5hPptgnNEC.png', 10230, 3, 3900, 9290, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(36828, 'Asuna Yuuki', 'Sword Art Online', 'https://s4.anilist.co/file/anilistcdn/character/large/b36828-j5ib0adAzGMx.png', 10074, 3, 2680, 9450, 2000, '2026-11-19 22:34:18.226568+09', 'anime', 0),
	(206212, 'Percival', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b206212-EyBnNcsSdqMx.png', 163, 3, 2700, 14840, 2000, '2026-11-19 22:34:18.228094+09', 'anime', 0),
	(265209, 'Lancelot', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b265209-NAHBUq0SX3Vb.png', 262, 4, 4450, 17470, 4000, '2026-11-19 22:34:18.228094+09', 'anime', 0),
	(249281, 'Tristan Liones', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b249281-2UxCTm6D6NAa.png', 119, 4, 4120, 20090, 4000, '2026-11-19 22:34:18.228094+09', 'anime', 0),
	(282753, 'Gawain', 'The Seven Deadly Sins: Four Knights of the Apocalypse', 'https://s4.anilist.co/file/anilistcdn/character/large/b282753-gJ5R4nrEVQFJ.png', 48, 3, 3960, 9490, 2000, '2026-11-19 22:34:18.228094+09', 'anime', 0);

INSERT INTO card_pictures VALUES
	(1, 'https://cdn.myanimelist.net/images/characters/9/573715.jpg'),
	(5, 'https://cdn.myanimelist.net/images/characters/14/520178.jpg'),
	(6, 'https://cdn.myanimelist.net/images/characters/12/560567.jpg'),
	(11, 'https://cdn.myanimelist.net/images/characters/11/527548.jpg'),
	(12, 'https://cdn.myanimelist.net/images/characters/15/527660.jpg'),
	(13, 'https://cdn.myanimelist.net/images/characters/8/572816.jpg'),
	(14, 'https://cdn.myanimelist.net/images/characters/15/516300.jpg'),
	(17, 'https://cdn.myanimelist.net/images/characters/9/562071.jpg'),
	(27, 'https://cdn.myanimelist.net/images/characters/16/571432.jpg'),
	(28, 'https://cdn.myanimelist.net/images/characters/15/572060.jpg'),
	(30, 'https://cdn.myanimelist.net/images/characters/7/572784.jpg'),
	(31, 'https://cdn.myanimelist.net/images/characters/15/573889.jpg'),
	(58, 'https://cdn.myanimelist.net/images/characters/15/582919.jpg'),
	(61, 'https://cdn.myanimelist.net/images/characters/3/587352.jpg'),
	(62, 'https://cdn.myanimelist.net/images/characters/11/591211.jpg'),
	(68, 'https://cdn.myanimelist.net/images/characters/12/571236.jpg'),
	(71, 'https://cdn.myanimelist.net/images/characters/9/519723.jpg'),
	(80, 'https://cdn.myanimelist.net/images/characters/5/581707.jpg'),
	(86, 'https://cdn.myanimelist.net/images/characters/15/591392.jpg'),
	(94, 'https://cdn.myanimelist.net/images/characters/5/572783.jpg'),
	(207, 'https://cdn.myanimelist.net/images/characters/13/586764.jpg'),
	(209, 'https://cdn.myanimelist.net/images/characters/5/581844.jpg'),
	(305, 'https://cdn.myanimelist.net/images/characters/15/587386.jpg'),
	(422, 'https://cdn.myanimelist.net/images/characters/11/559485.jpg'),
	(424, 'https://cdn.myanimelist.net/images/characters/3/572983.jpg'),
	(458, 'https://cdn.myanimelist.net/images/characters/5/544322.jpg'),
	(498, 'https://cdn.myanimelist.net/images/characters/13/626911.jpg'),
	(723, 'https://cdn.myanimelist.net/images/characters/9/593655.jpg'),
	(908, 'https://cdn.myanimelist.net/images/characters/7/574797.jpg'),
	(913, 'https://cdn.myanimelist.net/images/characters/15/584579.jpg'),
	(1086, 'https://cdn.myanimelist.net/images/characters/15/613713.jpg'),
	(1111, 'https://cdn.myanimelist.net/images/characters/16/581186.jpg'),
	(2219, 'https://cdn.myanimelist.net/images/characters/13/633214.jpg'),
	(4003, 'https://cdn.myanimelist.net/images/characters/15/559134.jpg'),
	(6194, 'https://cdn.myanimelist.net/images/characters/15/557011.jpg'),
	(6356, 'https://cdn.myanimelist.net/images/characters/16/572620.jpg'),
	(11772, 'https://cdn.myanimelist.net/images/characters/11/580529.jpg'),
	(12055, 'https://cdn.myanimelist.net/images/characters/6/596448.jpg'),
	(12064, 'https://cdn.myanimelist.net/images/characters/11/514086.jpg'),
	(13020, 'https://cdn.myanimelist.net/images/characters/4/548616.jpg'),
	(13085, 'https://cdn.myanimelist.net/images/characters/13/571901.jpg'),
	(19492, 'https://cdn.myanimelist.net/images/characters/14/624947.jpg'),
	(22036, 'https://cdn.myanimelist.net/images/characters/16/559456.jpg'),
	(22037, 'https://cdn.myanimelist.net/images/characters/8/559451.jpg'),
	(22055, 'https://cdn.myanimelist.net/images/characters/6/571238.jpg'),
	(23602, 'https://cdn.myanimelist.net/images/characters/5/577543.jpg'),
	(36765, 'https://cdn.myanimelist.net/images/characters/11/542381.jpg'),
	(38005, 'https://cdn.myanimelist.net/images/characters/16/526748.jpg'),
	(40881, 'https://cdn.myanimelist.net/images/characters/6/557557.jpg'),
	(40882, 'https://cdn.myanimelist.net/images/characters/4/577557.jpg');
INSERT INTO card_pictures VALUES
	(45627, 'https://cdn.myanimelist.net/images/characters/8/573458.jpg'),
	(46484, 'https://cdn.myanimelist.net/images/characters/6/618991.jpg'),
	(46494, 'https://cdn.myanimelist.net/images/characters/2/527625.jpg'),
	(46496, 'https://cdn.myanimelist.net/images/characters/14/559023.jpg'),
	(55131, 'https://cdn.myanimelist.net/images/characters/7/514123.jpg'),
	(64771, 'https://cdn.myanimelist.net/images/characters/10/504337.jpg'),
	(65865, 'https://cdn.myanimelist.net/images/characters/8/541508.jpg'),
	(66173, 'https://cdn.myanimelist.net/images/characters/12/507110.jpg'),
	(67065, 'https://cdn.myanimelist.net/images/characters/7/489512.jpg'),
	(67067, 'https://cdn.myanimelist.net/images/characters/14/626913.jpg'),
	(70069, 'https://cdn.myanimelist.net/images/characters/7/579201.jpg'),
	(71267, 'https://cdn.myanimelist.net/images/characters/7/484717.jpg'),
	(73935, 'https://cdn.myanimelist.net/images/characters/8/613709.jpg'),
	(77605, 'https://cdn.myanimelist.net/images/characters/4/600723.jpg'),
	(80243, 'https://cdn.myanimelist.net/images/characters/2/575593.jpg'),
	(81929, 'https://cdn.myanimelist.net/images/characters/9/590219.jpg'),
	(83797, 'https://cdn.myanimelist.net/images/characters/15/581712.jpg'),
	(84677, 'https://cdn.myanimelist.net/images/characters/6/572452.jpg'),
	(87275, 'https://cdn.myanimelist.net/images/characters/5/561870.jpg'),
	(88348, 'https://cdn.myanimelist.net/images/characters/10/647597.jpg'),
	(88349, 'https://cdn.myanimelist.net/images/characters/5/591593.jpg'),
	(88350, 'https://cdn.myanimelist.net/images/characters/3/640889.jpg'),
	(88572, 'https://cdn.myanimelist.net/images/characters/4/599371.jpg'),
	(88573, 'https://cdn.myanimelist.net/images/characters/9/556016.jpg'),
	(88575, 'https://cdn.myanimelist.net/images/characters/7/585332.jpg'),
	(88892, 'https://cdn.myanimelist.net/images/characters/4/501759.jpg'),
	(89028, 'https://cdn.myanimelist.net/images/characters/11/496155.jpg'),
	(89198, 'https://cdn.myanimelist.net/images/characters/7/521725.jpg'),
	(89225, 'https://cdn.myanimelist.net/images/characters/8/621382.jpg'),
	(89334, 'https://cdn.myanimelist.net/images/characters/7/517362.jpg'),
	(89361, 'https://cdn.myanimelist.net/images/characters/15/571690.jpg'),
	(89362, 'https://cdn.myanimelist.net/images/characters/4/571469.jpg'),
	(89364, 'https://cdn.myanimelist.net/images/characters/15/573711.jpg'),
	(120649, 'https://cdn.myanimelist.net/images/characters/14/568195.jpg'),
	(121101, 'https://cdn.myanimelist.net/images/characters/14/509811.jpg'),
	(121102, 'https://cdn.myanimelist.net/images/characters/9/509809.jpg'),
	(121103, 'https://cdn.myanimelist.net/images/characters/2/585976.jpg'),
	(121104, 'https://cdn.myanimelist.net/images/characters/7/568175.jpg'),
	(121889, 'https://cdn.myanimelist.net/images/characters/3/586182.jpg'),
	(121956, 'https://cdn.myanimelist.net/images/characters/8/531471.jpg'),
	(123212, 'https://cdn.myanimelist.net/images/characters/8/625712.jpg'),
	(123285, 'https://cdn.myanimelist.net/images/characters/6/602307.jpg'),
	(123962, 'https://cdn.myanimelist.net/images/characters/16/495971.jpg'),
	(124142, 'https://cdn.myanimelist.net/images/characters/14/606014.jpg'),
	(124381, 'https://cdn.myanimelist.net/images/characters/14/559013.jpg'),
	(124440, 'https://cdn.myanimelist.net/images/characters/10/599705.jpg'),
	(125886, 'https://cdn.myanimelist.net/images/characters/14/614437.jpg'),
	(126071, 'https://cdn.myanimelist.net/images/characters/10/520946.jpg'),
	(126372, 'https://cdn.myanimelist.net/images/characters/8/606509.jpg'),
	(126373, 'https://cdn.myanimelist.net/images/characters/13/585959.jpg');
INSERT INTO card_pictures VALUES
	(126375, 'https://cdn.myanimelist.net/images/characters/7/606473.jpg'),
	(126635, 'https://cdn.myanimelist.net/images/characters/16/552264.jpg'),
	(126824, 'https://cdn.myanimelist.net/images/characters/6/543444.jpg'),
	(127212, 'https://cdn.myanimelist.net/images/characters/9/621888.jpg'),
	(127221, 'https://cdn.myanimelist.net/images/characters/11/572437.jpg'),
	(127222, 'https://cdn.myanimelist.net/images/characters/4/585960.jpg'),
	(127518, 'https://cdn.myanimelist.net/images/characters/6/512287.jpg'),
	(127691, 'https://cdn.myanimelist.net/images/characters/2/581421.jpg'),
	(129133, 'https://cdn.myanimelist.net/images/characters/11/520941.jpg'),
	(129571, 'https://cdn.myanimelist.net/images/characters/10/618734.jpg'),
	(130102, 'https://cdn.myanimelist.net/images/characters/4/533495.jpg'),
	(133676, 'https://cdn.myanimelist.net/images/characters/2/609038.jpg'),
	(133699, 'https://cdn.myanimelist.net/images/characters/3/619366.jpg'),
	(133700, 'https://cdn.myanimelist.net/images/characters/6/581422.jpg'),
	(133704, 'https://cdn.myanimelist.net/images/characters/16/581424.jpg'),
	(134167, 'https://cdn.myanimelist.net/images/characters/3/620735.jpg'),
	(136070, 'https://cdn.myanimelist.net/images/characters/10/520945.jpg'),
	(136071, 'https://cdn.myanimelist.net/images/characters/7/580482.jpg'),
	(137079, 'https://cdn.myanimelist.net/images/characters/11/494277.jpg'),
	(137080, 'https://cdn.myanimelist.net/images/characters/2/520177.jpg'),
	(137081, 'https://cdn.myanimelist.net/images/characters/12/533494.jpg'),
	(137975, 'https://cdn.myanimelist.net/images/characters/9/633020.jpg'),
	(138100, 'https://cdn.myanimelist.net/images/characters/7/577712.jpg'),
	(138101, 'https://cdn.myanimelist.net/images/characters/9/614259.jpg'),
	(138102, 'https://cdn.myanimelist.net/images/characters/7/577709.jpg'),
	(141060, 'https://cdn.myanimelist.net/images/characters/13/642709.jpg'),
	(141061, 'https://cdn.myanimelist.net/images/characters/8/642707.jpg'),
	(145341, 'https://cdn.myanimelist.net/images/characters/12/514423.jpg'),
	(148740, 'https://cdn.myanimelist.net/images/characters/10/618421.jpg'),
	(154224, 'https://cdn.myanimelist.net/images/characters/3/611525.jpg'),
	(162722, 'https://cdn.myanimelist.net/images/characters/3/563859.jpg'),
	(174873, 'https://cdn.myanimelist.net/images/characters/10/642710.jpg'),
	(176754, 'https://cdn.myanimelist.net/images/characters/6/556987.jpg'),
	(183965, 'https://cdn.myanimelist.net/images/characters/16/561358.jpg'),
	(188783, 'https://cdn.myanimelist.net/images/characters/10/620416.jpg'),
	(222594, 'https://cdn.myanimelist.net/images/characters/4/593656.jpg'),
	(257562, 'https://cdn.myanimelist.net/images/characters/7/507758.jpg'),
	(260329, 'https://cdn.myanimelist.net/images/characters/8/518856.jpg');

ALTER TABLE arena_battles ENABLE ROW LEVEL SECURITY;
ALTER TABLE arena_players ENABLE ROW LEVEL SECURITY;
ALTER TABLE arena_teams ENABLE ROW LEVEL SECURITY;
ALTER TABLE arena_weeks ENABLE ROW LEVEL SECURITY;
ALTER TABLE ban_votes ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_stickers ENABLE ROW LEVEL SECURITY;
ALTER TABLE broadcasts ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_chats ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_checklists ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_anime_rewards ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_boxes ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_catalog ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_duels ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_mail ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_owned ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_pictures ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_players ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_quests ENABLE ROW LEVEL SECURITY;
ALTER TABLE card_trades ENABLE ROW LEVEL SECURITY;
ALTER TABLE channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE chats ENABLE ROW LEVEL SECURITY;
ALTER TABLE command_ranks ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE crystal_purchases ENABLE ROW LEVEL SECURITY;
ALTER TABLE game_scores ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_boosts ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE group_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE image_generations ENABLE ROW LEVEL SECURITY;
ALTER TABLE invite_awards ENABLE ROW LEVEL SECURITY;
ALTER TABLE join_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE mafia_games ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_ranks ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_reputation ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_warns ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE moderation_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE own_sticker_sets ENABLE ROW LEVEL SECURITY;
ALTER TABLE owner_notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE picture_verdicts ENABLE ROW LEVEL SECURITY;
ALTER TABLE premium_users ENABLE ROW LEVEL SECURITY;
ALTER TABLE private_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE raid_bosses ENABLE ROW LEVEL SECURITY;
ALTER TABLE raid_heroes ENABLE ROW LEVEL SECURITY;
ALTER TABLE raid_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE rank_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE reminders ENABLE ROW LEVEL SECURITY;
ALTER TABLE reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE reputation_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE skipped_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE song_recognitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE star_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE sticker_sends ENABLE ROW LEVEL SECURITY;
ALTER TABLE sticker_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE team_lobbies ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
