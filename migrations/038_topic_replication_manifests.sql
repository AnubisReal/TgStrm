-- A complete manifest is evidence for review, never permission to send by itself.
ALTER TABLE tgstrm.topics ADD CONSTRAINT topics_id_chat_id_unique UNIQUE (id, chat_id);
CREATE TABLE tgstrm.topic_replication_manifests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE RESTRICT,
 source_chat_id bigint NOT NULL,
 source_topic_id bigint NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 status text NOT NULL CHECK (status IN ('pending','scan_failed','excluded_nonvideo','review','empty','eligible')),
 evidence jsonb NOT NULL CHECK (jsonb_typeof(evidence)='object'),
 video_ids jsonb NOT NULL CHECK (jsonb_typeof(video_ids)='array'),
 upper_message_id bigint NOT NULL DEFAULT 0 CHECK (upper_message_id >= 0),
 scanned_at timestamptz NOT NULL DEFAULT now(),
 invalidated_at timestamptz,
 FOREIGN KEY (source_chat_id,account_id) REFERENCES tgstrm.chats(id,account_id) ON DELETE RESTRICT,
 FOREIGN KEY (source_topic_id,source_chat_id) REFERENCES tgstrm.topics(id,chat_id) ON DELETE RESTRICT,
 UNIQUE(account_id,source_topic_id)
);
CREATE INDEX topic_replication_manifests_source_chat_idx ON tgstrm.topic_replication_manifests(source_chat_id);
CREATE INDEX topic_replication_manifests_topic_chat_idx ON tgstrm.topic_replication_manifests(source_topic_id,source_chat_id);
