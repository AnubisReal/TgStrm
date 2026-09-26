ALTER TABLE tgstrm.topics
 ADD COLUMN is_active boolean NOT NULL DEFAULT true,
 ADD COLUMN is_pinned boolean NOT NULL DEFAULT false,
 ADD COLUMN unread_count integer NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
 ADD COLUMN unread_mentions_count integer NOT NULL DEFAULT 0 CHECK (unread_mentions_count >= 0),
 ADD COLUMN telegram_created_at timestamptz,
 ADD COLUMN last_message_at timestamptz;

CREATE INDEX topics_chat_active_activity
 ON tgstrm.topics(chat_id, is_active, top_message_id DESC, telegram_topic_id DESC);
