-- Durable, bounded normalization for videos uploaded directly to managed movie scopes.
CREATE TABLE tgstrm.movie_caption_jobs (
 message_id bigint PRIMARY KEY REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 tmdb_id bigint NOT NULL CHECK (tmdb_id>0),
 previous_caption text NOT NULL,
 desired_caption text NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','attention')),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX movie_caption_jobs_pending ON tgstrm.movie_caption_jobs(next_attempt_at) WHERE status='pending';
-- Own normalization changes presentation, not identity. Keep automatic matches
-- only for the exact recorded caption transition with an unchanged filename.
CREATE OR REPLACE FUNCTION tgstrm.invalidate_movie_match_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.source_signature IS DISTINCT FROM NEW.source_signature THEN
  IF EXISTS (SELECT 1 FROM tgstrm.movie_caption_jobs j
    JOIN tgstrm.tmdb_matches tm ON tm.message_id=j.message_id AND tm.media_type='movie' AND tm.tmdb_id=j.tmdb_id
    JOIN tgstrm.messages m ON m.id=j.message_id
    LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
    WHERE j.message_id=NEW.message_id AND j.status IN ('pending','completed')
    AND m.body=j.desired_caption
    AND j.desired_caption=trim(tm.title)||' ('||to_char(tm.release_date,'YYYY')||')'
    AND OLD.source_signature IN (sha256(convert_to(j.previous_caption,'UTF8')||decode('00','hex')||convert_to(COALESCE(mm.file_name,''),'UTF8')),decode(md5(j.previous_caption||chr(31)||COALESCE(mm.file_name,'')),'hex'))
    AND NEW.source_signature=sha256(convert_to(j.desired_caption,'UTF8')||decode('00','hex')||convert_to(COALESCE(mm.file_name,''),'UTF8'))) THEN
   UPDATE tgstrm.tmdb_scan_states SET source_text=(SELECT body FROM tgstrm.messages WHERE id=NEW.message_id),checked_at=now(),updated_at=now() WHERE message_id=NEW.message_id;
  ELSE
   DELETE FROM tgstrm.tmdb_matches tm WHERE tm.message_id=NEW.message_id
    AND EXISTS (SELECT 1 FROM tgstrm.tmdb_scan_states ss WHERE ss.message_id=NEW.message_id);
   DELETE FROM tgstrm.tmdb_scan_states WHERE message_id=NEW.message_id;
  END IF;
  NEW.processed_signature := NULL;
  NEW.processed_at := NULL;
 END IF;
 RETURN NEW;
END $$;
