ALTER TABLE tgstrm.topics
  ADD COLUMN library_kind text NOT NULL DEFAULT 'unclassified'
    CHECK (library_kind IN ('unclassified','movie_collection','series','ignored')),
  ADD COLUMN classification_source text NOT NULL DEFAULT 'automatic'
    CHECK (classification_source IN ('automatic','manual')),
  ADD COLUMN classified_at timestamptz;

UPDATE tgstrm.topics t SET
  library_kind = CASE
    WHEN lower(trim(t.title)) IN ('peliculas','películas','movies','films') THEN 'movie_collection'
    WHEN lower(trim(t.title)) = 'general' THEN 'ignored'
    ELSE 'series'
  END,
  classified_at = now()
FROM tgstrm.settings s
WHERE s.key='active_chat_id' AND (s.value #>> '{}')=t.chat_id::text;

DELETE FROM tgstrm.tmdb_matches tm USING tgstrm.topics t
WHERE tm.topic_id=t.id AND tm.message_id IS NULL AND t.library_kind<>'series';

CREATE INDEX topics_primary_library_kind ON tgstrm.topics(chat_id,library_kind) WHERE is_active;
