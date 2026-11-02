-- Persist former paths before changing the current export identity.
CREATE TABLE tgstrm.retired_export_paths (
 library_id uuid NOT NULL REFERENCES tgstrm.library_targets(id) ON DELETE RESTRICT,
 asset_id uuid NOT NULL REFERENCES tgstrm.stream_assets(id) ON DELETE RESTRICT,
 relative_path text NOT NULL,
 content_hash bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(library_id,asset_id,relative_path)
);
-- A match correction must survive browser closure and server restart.
CREATE TABLE tgstrm.topic_identity_jobs (
 topic_id bigint PRIMARY KEY REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 revision bigint NOT NULL DEFAULT 1,
 desired_title text NOT NULL,
 previous_title text NOT NULL,
 tmdb_id bigint NOT NULL,
 poster_path text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','attention')),
 last_error text,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX topic_identity_pending ON tgstrm.topic_identity_jobs(next_attempt_at) WHERE status='pending';
CREATE FUNCTION tgstrm.queue_topic_identity_correction() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE desired text;
BEGIN
 IF NEW.message_id IS NULL AND NEW.media_type='tv'
 AND (OLD.tmdb_id IS DISTINCT FROM NEW.tmdb_id OR OLD.title IS DISTINCT FROM NEW.title OR OLD.release_date IS DISTINCT FROM NEW.release_date)
 AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.destination_topic_id=NEW.topic_id)
 THEN
  desired := COALESCE(NULLIF(NEW.title,''),NEW.original_title);
  IF NEW.release_date IS NOT NULL THEN desired := desired || ' (' || extract(year FROM NEW.release_date)::text || ')'; END IF;
  desired := desired || ' [tmdbid-' || NEW.tmdb_id::text || ']';
  INSERT INTO tgstrm.topic_identity_jobs(topic_id,desired_title,previous_title,tmdb_id,poster_path)
  VALUES(NEW.topic_id,desired,(SELECT title FROM tgstrm.topics WHERE id=NEW.topic_id),NEW.tmdb_id,COALESCE(NEW.poster_path,''))
  ON CONFLICT(topic_id) DO UPDATE SET revision=tgstrm.topic_identity_jobs.revision+1,desired_title=EXCLUDED.desired_title,previous_title=EXCLUDED.previous_title,tmdb_id=EXCLUDED.tmdb_id,poster_path=EXCLUDED.poster_path,status='pending',last_error=NULL,next_attempt_at=now(),updated_at=now();
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER tmdb_topic_identity_correction AFTER UPDATE ON tgstrm.tmdb_matches FOR EACH ROW EXECUTE FUNCTION tgstrm.queue_topic_identity_correction();
