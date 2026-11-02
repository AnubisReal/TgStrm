package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gotd/td/tgerr"
	"log"
	"strings"
	telegramauth "tgstrm/internal/telegram"
	"time"
	"unicode/utf8"
)

func normalizedMovieCaption(title, year string) (string, bool) {
	title = strings.TrimSpace(title)
	if title == "" || len(year) != 4 {
		return "", false
	}
	for _, r := range year {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	caption := title + " (" + year + ")"
	return caption, utf8.RuneCountInString(caption) <= 1024
}

const directMovieCaptionsSQL = `SELECT m.id::text,tm.tmdb_id,COALESCE(m.body,''),trim(tm.title),to_char(tm.release_date,'YYYY')
 FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
 JOIN tgstrm.managed_catalog_chats c ON c.id=m.chat_id
 JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id AND tm.media_type='movie'
 WHERE m.is_active AND m.is_outgoing AND m.media_type='video' AND t.is_active
 AND t.library_kind='movie_collection' AND c.is_active AND c.telegram_peer_type='channel'
 AND tm.tmdb_id>0 AND tm.release_date IS NOT NULL AND trim(tm.title)<>''
 AND m.body IS DISTINCT FROM trim(tm.title)||' ('||to_char(tm.release_date,'YYYY')||')'
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries d WHERE d.destination_message_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.catalog_import_messages d WHERE d.destination_message_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.mirror_deliveries d WHERE d.destination_message_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_caption_jobs j WHERE j.message_id=m.id AND j.tmdb_id=tm.tmdb_id
 AND j.previous_caption=m.body AND j.desired_caption=trim(tm.title)||' ('||to_char(tm.release_date,'YYYY')||')')
 ORDER BY m.id LIMIT 50`

func (a *API) prepareDirectMovieCaptions(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, directMovieCaptionsSQL)
	if err != nil {
		return err
	}
	type candidate struct {
		id, previous, desired string
		tmdb                  int64
	}
	var jobs []candidate
	for rows.Next() {
		var j candidate
		var title, year string
		if err = rows.Scan(&j.id, &j.tmdb, &j.previous, &title, &year); err != nil {
			rows.Close()
			return err
		}
		if caption, ok := normalizedMovieCaption(title, year); ok {
			j.desired = caption
			jobs = append(jobs, j)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.movie_caption_jobs(message_id,tmdb_id,previous_caption,desired_caption)
 VALUES($1::bigint,$2,$3,$4) ON CONFLICT(message_id) DO UPDATE SET tmdb_id=EXCLUDED.tmdb_id,
 previous_caption=EXCLUDED.previous_caption,desired_caption=EXCLUDED.desired_caption,status='pending',last_error=NULL,next_attempt_at=now(),updated_at=now()
 WHERE (movie_caption_jobs.tmdb_id,movie_caption_jobs.previous_caption,movie_caption_jobs.desired_caption)
 IS DISTINCT FROM (EXCLUDED.tmdb_id,EXCLUDED.previous_caption,EXCLUDED.desired_caption)`, j.id, j.tmdb, j.previous, j.desired)
		if err != nil {
			return err
		}
	}
	return nil
}

func movieCaptionNeedsAttention(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, telegramauth.ErrTopicIdentityConflict) || tgerr.Is(err, "MESSAGE_AUTHOR_REQUIRED", "MESSAGE_EDIT_TIME_EXPIRED", "CHAT_ADMIN_REQUIRED", "CHAT_WRITE_FORBIDDEN", "CHANNEL_PRIVATE", "MESSAGE_ID_INVALID", "MEDIA_CAPTION_TOO_LONG")
}

func (a *API) processDirectMovieCaption(ctx context.Context) error {
	service := a.telegramService()
	if service == nil {
		return nil
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	// Share the delivery lock: normalization cannot overlap movie forwarding.
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('tgstrm-movie-delivery-worker'))`).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlock, `SELECT pg_advisory_unlock(hashtext('tgstrm-movie-delivery-worker'))`)
	}()
	var id, previous, desired string
	var tmdb int64
	err = conn.QueryRowContext(ctx, `SELECT j.message_id::text,j.tmdb_id,j.previous_caption,j.desired_caption FROM tgstrm.movie_caption_jobs j
 JOIN tgstrm.messages m ON m.id=j.message_id WHERE j.status='pending' AND j.next_attempt_at<=now() AND m.is_active
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_caption_jobs waiting WHERE waiting.status='pending' AND waiting.last_error IS NOT NULL AND waiting.next_attempt_at>now())
 ORDER BY j.updated_at,j.message_id LIMIT 1`).Scan(&id, &tmdb, &previous, &desired)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = service.CorrectManagedMovieCaption(ctx, id, tmdb, previous, desired)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := "pending"
		if movieCaptionNeedsAttention(err) {
			status = "attention"
		}
		_, saveErr := conn.ExecContext(ctx, `UPDATE tgstrm.movie_caption_jobs SET status=$2,last_error=$3,next_attempt_at=now()+($4::double precision*interval '1 second'),updated_at=now() WHERE message_id::text=$1 AND tmdb_id=$5 AND previous_caption=$6 AND desired_caption=$7`, id, status, err.Error(), movieRetryDelay(err).Seconds(), tmdb, previous, desired)
		if saveErr != nil {
			return saveErr
		}
		return nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var topic string
	err = tx.QueryRowContext(ctx, `UPDATE tgstrm.messages m SET body=$2,updated_at=now() WHERE m.id::text=$1 AND m.is_active AND m.body IN ($3,$2)
 AND EXISTS(SELECT 1 FROM tgstrm.tmdb_matches tm WHERE tm.message_id=m.id AND tm.media_type='movie' AND tm.tmdb_id=$4
 AND trim(tm.title)||' ('||to_char(tm.release_date,'YYYY')||')'=$2) RETURNING m.topic_id::text`, id, desired, previous, tmdb).Scan(&topic)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_caption_jobs SET status='attention',last_error='El archivo o su coincidencia cambió durante la normalización',updated_at=now() WHERE message_id::text=$1 AND tmdb_id=$2 AND previous_caption=$3 AND desired_caption=$4`, id, tmdb, previous, desired)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	// The exact transition is recognized by the source invalidation trigger.
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.stream_assets sa SET source_signature=sha256(convert_to(m.body,'UTF8')||decode('00','hex')||convert_to(COALESCE(mm.file_name,''),'UTF8')),updated_at=now()
 FROM tgstrm.messages m LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE sa.message_id=m.id AND m.id::text=$1`, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_caption_jobs SET status='completed',last_error=NULL,updated_at=now() WHERE message_id::text=$1 AND tmdb_id=$2 AND previous_caption=$3 AND desired_caption=$4`, id, tmdb, previous, desired)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('reason','movie_caption_normalized','message_id',$2::text))`, topic, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *API) runDirectMovieCaptionWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			step, cancel := context.WithTimeout(ctx, 45*time.Second)
			if err := a.prepareDirectMovieCaptions(step); err != nil && step.Err() == nil {
				log.Printf("prepare direct movie captions: %v", err)
			}
			if err := a.processDirectMovieCaption(step); err != nil && step.Err() == nil {
				log.Printf("normalize direct movie caption: %v", err)
			}
			cancel()
		}
	}
}
