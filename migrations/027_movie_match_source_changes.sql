-- A caption or filename edit changes the evidence used to identify a movie.
-- Automatically inferred matches must therefore be reconsidered instead of
-- keeping a stale title forever. The next background pass performs the scan.
CREATE FUNCTION tgstrm.invalidate_movie_match_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.source_signature IS DISTINCT FROM NEW.source_signature THEN
    DELETE FROM tgstrm.tmdb_matches tm
    WHERE tm.message_id=NEW.message_id
      AND EXISTS (SELECT 1 FROM tgstrm.tmdb_scan_states ss WHERE ss.message_id=NEW.message_id);
    DELETE FROM tgstrm.tmdb_scan_states WHERE message_id=NEW.message_id;
    NEW.processed_signature := NULL;
    NEW.processed_at := NULL;
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER stream_assets_invalidate_movie_match_source
BEFORE UPDATE OF source_signature ON tgstrm.stream_assets
FOR EACH ROW EXECUTE FUNCTION tgstrm.invalidate_movie_match_source();
