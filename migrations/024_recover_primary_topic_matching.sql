-- Topics discovered after an offline interval used to keep the default
-- 'unclassified' value and were therefore invisible to automatic TV matching.
UPDATE tgstrm.topics t SET
  library_kind = CASE
    WHEN lower(trim(t.title)) IN ('peliculas','películas','movies','films') THEN 'movie_collection'
    WHEN lower(trim(t.title))='general' THEN 'ignored'
    ELSE 'series'
  END,
  classification_source='automatic',classified_at=now(),updated_at=now()
FROM tgstrm.settings s
WHERE s.key='active_chat_id' AND (s.value #>> '{}')=t.chat_id::text
  AND t.classification_source='automatic' AND t.library_kind='unclassified';

-- A transient TMDB/network failure must not permanently suppress recovery.
UPDATE tgstrm.tmdb_topic_scan_states
SET checked_at=now()-interval '6 minutes',updated_at=now()
WHERE status='failed';
