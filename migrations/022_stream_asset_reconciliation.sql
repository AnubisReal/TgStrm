ALTER TABLE tgstrm.stream_assets
  ADD COLUMN source_signature bytea,
  ADD COLUMN processed_signature bytea,
  ADD COLUMN processed_at timestamptz,
  ADD COLUMN last_export_error text;

UPDATE tgstrm.stream_assets sa SET
  source_signature=decode(md5(COALESCE(m.body,'') || chr(31) || COALESCE(mm.file_name,'')),'hex'),
  processed_signature=CASE WHEN EXISTS(
    SELECT 1 FROM tgstrm.stream_exports e WHERE e.asset_id=sa.id AND e.status='written'
  ) THEN decode(md5(COALESCE(m.body,'') || chr(31) || COALESCE(mm.file_name,'')),'hex') END,
  processed_at=CASE WHEN EXISTS(
    SELECT 1 FROM tgstrm.stream_exports e WHERE e.asset_id=sa.id AND e.status='written'
  ) THEN now() END
FROM tgstrm.messages m
LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
WHERE m.id=sa.message_id;

ALTER TABLE tgstrm.stream_assets ALTER COLUMN source_signature SET NOT NULL;
CREATE INDEX stream_assets_pending_export
  ON tgstrm.stream_assets(updated_at)
  WHERE processed_signature IS DISTINCT FROM source_signature;

CREATE FUNCTION tgstrm.invalidate_topic_stream_assets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.title IS DISTINCT FROM NEW.title OR OLD.library_kind IS DISTINCT FROM NEW.library_kind THEN
    UPDATE tgstrm.stream_assets sa SET processed_signature=NULL,processed_at=NULL,updated_at=now()
    FROM tgstrm.messages m WHERE m.id=sa.message_id AND m.topic_id=NEW.id AND m.is_active;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER topics_invalidate_stream_assets
AFTER UPDATE OF title,library_kind ON tgstrm.topics
FOR EACH ROW EXECUTE FUNCTION tgstrm.invalidate_topic_stream_assets();

CREATE FUNCTION tgstrm.invalidate_tmdb_stream_assets() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE affected_topic bigint;
BEGIN
  affected_topic := COALESCE(NEW.topic_id,OLD.topic_id);
  IF COALESCE(NEW.message_id,OLD.message_id) IS NULL THEN
    UPDATE tgstrm.stream_assets sa SET processed_signature=NULL,processed_at=NULL,updated_at=now()
    FROM tgstrm.messages m WHERE m.id=sa.message_id AND m.topic_id=affected_topic AND m.is_active;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER tmdb_matches_invalidate_stream_assets
AFTER INSERT OR UPDATE OR DELETE ON tgstrm.tmdb_matches
FOR EACH ROW EXECUTE FUNCTION tgstrm.invalidate_tmdb_stream_assets();
