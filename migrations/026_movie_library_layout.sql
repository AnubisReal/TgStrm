-- Movie matches belong to individual messages. Invalidate that asset whenever
-- its confirmed identity changes so automatic export uses the new canonical
-- title/year path just like a series topic match does for all its episodes.
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
  ELSE
    UPDATE tgstrm.stream_assets SET processed_signature=NULL,processed_at=NULL,updated_at=now()
    WHERE message_id=affected_message;
  END IF;
  RETURN NULL;
END $$;

-- Re-plan existing movie exports once. The reconciler will write the canonical
-- destination first and remove the former shared-folder file afterwards.
UPDATE tgstrm.stream_assets sa
SET processed_signature=NULL,processed_at=NULL,updated_at=now()
FROM tgstrm.messages m
JOIN tgstrm.topics t ON t.id=m.topic_id
WHERE sa.message_id=m.id AND m.is_active AND t.is_active
  AND t.library_kind='movie_collection'
  AND EXISTS (SELECT 1 FROM tgstrm.stream_exports e WHERE e.asset_id=sa.id);
