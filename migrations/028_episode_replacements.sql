INSERT INTO tgstrm.settings(key,value)
VALUES('episode_replacement_started_at',to_jsonb(now()))
ON CONFLICT(key) DO NOTHING;

CREATE TABLE tgstrm.episode_replacements (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 old_message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 new_message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','failed')),
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz,
 UNIQUE(old_message_id,new_message_id),
 CHECK(old_message_id<>new_message_id)
);

CREATE INDEX episode_replacements_retry
 ON tgstrm.episode_replacements(updated_at)
 WHERE status IN ('pending','failed');
