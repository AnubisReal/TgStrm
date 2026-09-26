-- Make the existing catalog import workflow a durable queue. The import and
-- its complete source manifest are committed before the HTTP request returns;
-- no second jobs table is introduced.
ALTER TABLE tgstrm.catalog_imports
 ADD COLUMN destination_title text,
 ADD COLUMN tmdb_payload jsonb,
 ADD COLUMN queued_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN started_at timestamptz,
 ADD COLUMN attempt_count integer NOT NULL DEFAULT 0,
 ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE tgstrm.catalog_import_messages
 ADD COLUMN selected_for_run boolean NOT NULL DEFAULT true;

UPDATE tgstrm.catalog_imports ci
SET destination_title=dt.title
FROM tgstrm.topics dt
WHERE dt.id=ci.destination_topic_id AND ci.destination_title IS NULL;

CREATE INDEX catalog_imports_queue_ready
 ON tgstrm.catalog_imports(next_attempt_at,queued_at,id)
 WHERE status='pending';
