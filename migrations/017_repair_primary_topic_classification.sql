UPDATE tgstrm.topics t SET
  library_kind = CASE
    WHEN lower(trim(t.title)) IN ('peliculas','películas','movies','films') THEN 'movie_collection'
    WHEN lower(trim(t.title))='general' THEN 'ignored'
    ELSE 'series'
  END,
  classification_source='automatic',classified_at=now(),updated_at=now()
WHERE t.classification_source<>'manual'
AND EXISTS (
  SELECT 1 FROM tgstrm.settings s
  WHERE s.key='active_chat_id'
  AND trim(both '"' from s.value::text)=t.chat_id::text
);

DELETE FROM tgstrm.tmdb_topic_scan_states ss USING tgstrm.topics t
WHERE ss.topic_id=t.id AND t.library_kind='series'
AND NOT EXISTS (SELECT 1 FROM tgstrm.tmdb_matches tm WHERE tm.topic_id=t.id AND tm.message_id IS NULL);
