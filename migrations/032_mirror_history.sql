-- Existing links keep their current "new files only" semantics. New links may
-- opt into a durable historical scan without creating a second workflow.
ALTER TABLE tgstrm.mirror_links
 ADD COLUMN history_mode text NOT NULL DEFAULT 'new_only' CHECK(history_mode IN ('new_only','include_existing')),
 ADD COLUMN history_status text NOT NULL DEFAULT 'not_requested' CHECK(history_status IN ('not_requested','pending','indexing','copying','completed','failed')),
 ADD COLUMN history_total integer NOT NULL DEFAULT 0,
 ADD COLUMN history_completed integer NOT NULL DEFAULT 0,
 ADD COLUMN history_attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN history_next_attempt_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN history_last_error text;

CREATE INDEX mirror_links_history_pending
 ON tgstrm.mirror_links(history_next_attempt_at,created_at)
 WHERE status='active' AND history_status IN ('pending','indexing','failed');
