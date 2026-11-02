-- Explicit copies outside the catalogue never enroll their destination in
-- library synchronization, matching, STRM generation or mirror deletion.
CREATE TABLE tgstrm.file_transfers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 source_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id),
 destination_chat_id bigint NOT NULL REFERENCES tgstrm.chats(id),
 destination_topic_id bigint REFERENCES tgstrm.topics(id),
 message_ids bigint[] NOT NULL,
 completed_count integer NOT NULL DEFAULT 0,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','attention','cancelled')),
 last_error text,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(cardinality(message_ids)>0), CHECK(completed_count BETWEEN 0 AND cardinality(message_ids))
);
CREATE INDEX file_transfers_pending ON tgstrm.file_transfers(next_attempt_at) WHERE status='pending';
