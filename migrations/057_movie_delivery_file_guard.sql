-- One owner per exact Telegram document and destination scope. Existing copies
-- remain untouched; only the earliest indexed delivery becomes the owner.
CREATE TABLE tgstrm.movie_delivery_files (
 destination_chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 destination_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 telegram_file_id bigint NOT NULL CHECK(telegram_file_id>0),
 delivery_id uuid NOT NULL UNIQUE REFERENCES tgstrm.movie_monitor_deliveries(id) ON DELETE RESTRICT,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(destination_chat_id,destination_topic_id,telegram_file_id)
);
INSERT INTO tgstrm.movie_delivery_files(destination_chat_id,destination_topic_id,telegram_file_id,delivery_id)
 SELECT DISTINCT ON (mon.destination_chat_id,mon.destination_topic_id,media.telegram_file_id)
 mon.destination_chat_id,mon.destination_topic_id,media.telegram_file_id,d.id
 FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.movie_monitors mon ON mon.id=d.monitor_id
 JOIN tgstrm.messages m ON m.id=d.destination_message_id AND m.is_active
 JOIN tgstrm.message_media media ON media.message_id=m.id
 WHERE mon.destination_topic_id IS NOT NULL AND media.telegram_file_id>0
 ORDER BY mon.destination_chat_id,mon.destination_topic_id,media.telegram_file_id,m.telegram_message_id;
CREATE INDEX message_media_telegram_file_lookup ON tgstrm.message_media(telegram_file_id,message_id) WHERE telegram_file_id IS NOT NULL;
