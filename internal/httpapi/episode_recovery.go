package httpapi

import "context"

// Bump when episode identification semantics change, so old errors are retried.
const episodeDetectorVersion = "2026-10-06-context-v3"

const episodeRecheckDueSQL = `(re.message_id IS NULL OR re.detector_version<>$1 OR re.source_signature IS DISTINCT FROM sa.source_signature OR re.next_attempt_at<=now())`

// Recheck stored errors using indexed metadata, never reread Telegram to decide.
// A still ambiguous source is checked at most daily, unless its signature or
// detector version changes. Reuse the durable caption/STRM correction queue.
func (a *API) recoverEpisodeDetection(ctx context.Context) {
	rows, err := a.db.QueryContext(ctx, `SELECT m.id::text,m.topic_id::text,COALESCE(mm.file_name,''),COALESCE(m.body,''),tm.tmdb_id,sa.source_signature
 FROM tgstrm.stream_assets sa JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active AND m.media_type='video'
 JOIN tgstrm.message_media mm ON mm.message_id=m.id
 JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
 JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id AND c.is_active
 JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL AND tm.media_type='tv'
 LEFT JOIN tgstrm.episode_detection_rechecks re ON re.message_id=m.id
 WHERE sa.last_export_error='No se pudo identificar temporada y episodio.'
 AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.destination_topic_id=t.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.topic_identity_jobs j WHERE j.topic_id=t.id AND j.status<>'completed')
 AND NOT EXISTS(SELECT 1 FROM tgstrm.settings paused WHERE paused.key='strm_cleanup_paused_groups' AND paused.value @> jsonb_build_object(c.id::text,true))
 AND `+episodeRecheckDueSQL+`
 ORDER BY re.next_attempt_at NULLS FIRST,m.id LIMIT 20`, episodeDetectorVersion)
	if err != nil {
		return
	}
	type item struct {
		id, topic, file, caption string
		series                   int64
		signature                []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.topic, &it.file, &it.caption, &it.series, &it.signature) != nil {
			rows.Close()
			return
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	queued := map[string]bool{}
	for _, it := range items {
		if queued[it.topic] {
			continue
		}
		_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.episode_detection_rechecks(message_id,source_signature,detector_version,next_attempt_at)
 VALUES($1::bigint,$2,$3,now()+interval '1 day') ON CONFLICT(message_id) DO UPDATE SET source_signature=EXCLUDED.source_signature,detector_version=EXCLUDED.detector_version,next_attempt_at=EXCLUDED.next_attempt_at,updated_at=now()`, it.id, it.signature, episodeDetectorVersion)
		if err != nil {
			return
		}
		if _, ok := a.detectSeriesEpisode(ctx, it.series, it.file, it.caption); !ok {
			continue
		}
		result, err := a.db.ExecContext(ctx, `INSERT INTO tgstrm.topic_identity_jobs(topic_id,desired_title,previous_title,tmdb_id,poster_path,identity_applied_revision)
 SELECT t.id,t.title,t.title,tm.tmdb_id,COALESCE(tm.poster_path,''),1 FROM tgstrm.topics t JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL
 WHERE t.id::text=$1 AND tm.tmdb_id=$2
 ON CONFLICT(topic_id) DO UPDATE SET captions_prepared_revision=0,status='pending',last_error=NULL,next_attempt_at=now(),updated_at=now()
 WHERE tgstrm.topic_identity_jobs.status='completed'`, it.topic, it.series)
		if err != nil {
			return
		}
		n, _ := result.RowsAffected()
		queued[it.topic] = n > 0
	}
}
