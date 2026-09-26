CREATE TABLE tgstrm.chats (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE CASCADE,
 telegram_peer_type text NOT NULL CHECK (telegram_peer_type IN ('chat','channel')),
 telegram_peer_id bigint NOT NULL,
 access_hash bigint,
 title text NOT NULL,
 username text,
 is_forum boolean NOT NULL DEFAULT false,
 is_active boolean NOT NULL DEFAULT true,
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(account_id, telegram_peer_type, telegram_peer_id)
);
CREATE TABLE tgstrm.topics (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE CASCADE,
 telegram_topic_id bigint NOT NULL,
 title text NOT NULL,
 icon_color integer, icon_emoji_id bigint,
 is_closed boolean NOT NULL DEFAULT false, is_hidden boolean NOT NULL DEFAULT false,
 top_message_id bigint,
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(chat_id, telegram_topic_id)
);
CREATE TABLE tgstrm.sync_states (
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE CASCADE,
 scope text NOT NULL, resource_id bigint NOT NULL DEFAULT 0,
 status text NOT NULL CHECK (status IN ('pending','running','ready','stale','failed')),
 cursor jsonb NOT NULL DEFAULT '{}'::jsonb, last_error text,
 started_at timestamptz, completed_at timestamptz, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(account_id, scope, resource_id)
);
CREATE INDEX chats_account_active ON tgstrm.chats(account_id, is_active, title);
CREATE INDEX topics_chat_activity ON tgstrm.topics(chat_id, top_message_id DESC, telegram_topic_id DESC);
