-- Additive recovery state. Never resets matches, selections, imports or exports.
CREATE TABLE tgstrm.manifest_deleted_messages (
 chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 telegram_message_id integer NOT NULL CHECK (telegram_message_id>0),
 received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(chat_id,telegram_message_id)
);
CREATE TABLE tgstrm.manifest_rechecks (
 manifest_id uuid PRIMARY KEY REFERENCES tgstrm.topic_replication_manifests(id) ON DELETE CASCADE,
 revision bigint NOT NULL DEFAULT 1,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX manifest_rechecks_due ON tgstrm.manifest_rechecks(next_attempt_at);
CREATE TABLE tgstrm.manifest_message_updates (
 chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 telegram_message_id integer NOT NULL CHECK(telegram_message_id>0),
 signature text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(chat_id,telegram_message_id)
);
