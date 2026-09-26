CREATE TABLE tgstrm.messages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE CASCADE,
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 telegram_message_id integer NOT NULL CHECK (telegram_message_id > 0),
 sender_peer_type text,
 sender_peer_id bigint,
 body text NOT NULL DEFAULT '',
 media_type text,
 reply_to_message_id integer,
 grouped_id bigint,
 sent_at timestamptz NOT NULL,
 edited_at timestamptz,
 is_outgoing boolean NOT NULL DEFAULT false,
 is_pinned boolean NOT NULL DEFAULT false,
 is_active boolean NOT NULL DEFAULT true,
 first_seen_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(chat_id, telegram_message_id)
);

CREATE INDEX messages_topic_page ON tgstrm.messages(topic_id, is_active, telegram_message_id DESC);
CREATE INDEX messages_chat_sender ON tgstrm.messages(chat_id, sender_peer_type, sender_peer_id) WHERE sender_peer_id IS NOT NULL;

CREATE TABLE tgstrm.message_media (
 message_id bigint PRIMARY KEY REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 media_type text NOT NULL,
 telegram_file_id bigint,
 access_hash bigint,
 file_reference bytea,
 file_name text,
 mime_type text,
 byte_size bigint CHECK (byte_size IS NULL OR byte_size >= 0),
 width integer CHECK (width IS NULL OR width >= 0),
 height integer CHECK (height IS NULL OR height >= 0),
 duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
 metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
 updated_at timestamptz NOT NULL DEFAULT now()
);
