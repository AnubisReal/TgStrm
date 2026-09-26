-- A source-caption update can delete a movie match from the BEFORE trigger on
-- stream_assets. That delete fires this trigger while the same row is already
-- being updated, which PostgreSQL correctly rejects. The outer trigger has
-- already reset the processed state, so nested invalidation is redundant.
CREATE OR REPLACE FUNCTION tgstrm.invalidate_tmdb_stream_assets() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  affected_topic bigint;
  affected_message bigint;
BEGIN
  affected_topic := COALESCE(NEW.topic_id,OLD.topic_id);
  affected_message := COALESCE(NEW.message_id,OLD.message_id);
  IF affected_message IS NULL THEN
    UPDATE tgstrm.stream_assets sa SET processed_signature=NULL,processed_at=NULL,updated_at=now()
    FROM tgstrm.messages m WHERE m.id=sa.message_id AND m.topic_id=affected_topic AND m.is_active;
  ELSIF pg_trigger_depth() = 1 THEN
    UPDATE tgstrm.stream_assets SET processed_signature=NULL,processed_at=NULL,updated_at=now()
    WHERE message_id=affected_message;
  END IF;
  RETURN NULL;
END $$;
