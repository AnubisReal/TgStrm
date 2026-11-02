package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gotd/td/tgerr"
	telegramauth "tgstrm/internal/telegram"
)

// Each stage is durable. A browser disconnect never owns the worker context.
func (a *API) runMovieMonitorWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			step, cancel := context.WithTimeout(ctx, 45*time.Second)
			a.scanMovieMonitors(step)
			for i := 0; i < 3 && step.Err() == nil; i++ {
				a.processMovieDeliveryLocked(step, true)
			}
			a.processMovieDelivery(step)
			cancel()
		}
	}
}

func movieRetryDelay(err error) time.Duration {
	if delay, ok := tgerr.AsFloodWait(err); ok {
		return delay + time.Second
	}
	return time.Minute
}

func (a *API) scanMovieMonitors(ctx context.Context) {
	service := a.telegramService()
	if service == nil {
		return
	}
	var forumMonitor, forumGroup string
	if err := a.db.QueryRowContext(ctx, `SELECT id::text,source_chat_id::text FROM tgstrm.movie_monitors WHERE status='active' AND source_topic_id IS NULL AND (last_topics_refreshed_at IS NULL OR last_topics_refreshed_at<now()-interval '5 minutes') ORDER BY last_topics_refreshed_at NULLS FIRST LIMIT 1`).Scan(&forumMonitor, &forumGroup); err == nil {
		refreshErr := service.RefreshTopics(ctx, forumGroup)
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitors SET last_topics_refreshed_at=now(),last_error=$2 WHERE id::text=$1`, forumMonitor, errorText(refreshErr))
	}
	// Whole forums acquire scopes for newly discovered topics as well as existing ones.
	_, err := a.db.ExecContext(ctx, `INSERT INTO tgstrm.movie_monitor_scopes(monitor_id,topic_id,boundary_id)
 SELECT mm.id,t.id,mm.start_after_message_id FROM tgstrm.movie_monitors mm JOIN tgstrm.topics t ON t.chat_id=mm.source_chat_id AND t.is_active
 WHERE mm.status='active' AND (mm.source_topic_id IS NULL OR mm.source_topic_id=t.id)
 ON CONFLICT DO NOTHING`)
	if err != nil {
		log.Printf("movie scope discovery: %v", err)
		return
	}
	var monitor, topic, group, lease string
	var cursor, upper, boundary, pass int
	err = a.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT s.monitor_id,s.topic_id FROM tgstrm.movie_monitor_scopes s JOIN tgstrm.movie_monitors mm ON mm.id=s.monitor_id
 WHERE mm.status='active' AND s.next_scan_at<=now() AND (s.lease_expires_at IS NULL OR s.lease_expires_at<now())
 ORDER BY s.next_scan_at FOR UPDATE OF s SKIP LOCKED LIMIT 1)
 UPDATE tgstrm.movie_monitor_scopes s SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes'
 FROM candidate c,tgstrm.movie_monitors mm WHERE s.monitor_id=c.monitor_id AND s.topic_id=c.topic_id AND mm.id=s.monitor_id
 RETURNING s.monitor_id::text,s.topic_id::text,mm.source_chat_id::text,s.lease_token::text,s.cursor_id,s.upper_id,s.boundary_id,s.scan_pass`).Scan(&monitor, &topic, &group, &lease, &cursor, &upper, &boundary, &pass)
	if err == nil {
		filter := "video"
		if pass == 1 {
			filter = "document"
		}
		low, high, readErr := service.ReadMovieScopePage(ctx, group, topic, cursor, filter)
		if readErr != nil {
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_scopes SET last_error=$4,next_scan_at=now()+$5::interval,lease_token=NULL,lease_expires_at=NULL WHERE monitor_id::text=$1 AND topic_id::text=$2 AND lease_token::text=$3`, monitor, topic, lease, readErr.Error(), movieRetryDelay(readErr).String())
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitors SET last_error=$2 WHERE id::text=$1`, monitor, readErr.Error())
			a.publishMovieMonitor(ctx, monitor)
			a.publishMovieMonitor(ctx, monitor)
		} else {
			if high > upper {
				upper = high
			}
			passDone := low == 0 || low <= boundary
			done := passDone && pass == 1
			delay := time.Second
			if done {
				cursor = 0
				boundary = upper
				upper = 0
				delay = time.Minute
				pass = 0
			} else if passDone {
				cursor = 0
				pass = 1
			} else {
				cursor = low
			}
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_scopes SET cursor_id=$4,upper_id=$5,boundary_id=$6,initial_scan_complete=initial_scan_complete OR $8,scan_pass=$9,next_scan_at=now()+$7::interval,last_error=NULL,lease_token=NULL,lease_expires_at=NULL WHERE monitor_id::text=$1 AND topic_id::text=$2 AND lease_token::text=$3`, monitor, topic, lease, cursor, upper, boundary, delay.String(), done, pass)
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitors SET last_error=NULL,last_scanned_at=now() WHERE id::text=$1`, monitor)
		}
	}
	// Discovery consumes the same persisted catalog as realtime updates. Stable
	// random IDs differ for presentation and video and survive every restart.
	_, discoverErr := a.db.ExecContext(ctx, `WITH candidates AS (
 SELECT gen_random_uuid() id,mm.id monitor_id,m.id message_id,encode(sa.source_signature,'hex') signature
 FROM tgstrm.movie_monitors mm JOIN tgstrm.messages m ON m.chat_id=mm.source_chat_id
 JOIN tgstrm.stream_assets sa ON sa.message_id=m.id JOIN tgstrm.topics t ON t.id=m.topic_id
 WHERE mm.status='active' AND m.is_active AND t.is_active AND m.media_type='video'
 AND (mm.source_topic_id IS NULL OR mm.source_topic_id=m.topic_id)
 AND (mm.history_mode='include_existing' OR (m.telegram_message_id>mm.start_after_message_id AND m.sent_at>=date_trunc('second',mm.created_at)))
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries d WHERE d.monitor_id=mm.id AND d.source_message_id=m.id))
 INSERT INTO tgstrm.movie_monitor_deliveries(id,monitor_id,source_message_id,source_signature,presentation_random_id,video_random_id)
 SELECT id,monitor_id,message_id,signature,(hashtextextended(id::text||':poster',0)&9223372036854775806)+1,
 (hashtextextended(id::text||':video',0)&9223372036854775806)+1 FROM candidates ON CONFLICT(monitor_id,source_message_id) DO NOTHING`)
	if discoverErr != nil {
		log.Printf("movie delivery discovery: %v", discoverErr)
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries d SET source_signature=encode(sa.source_signature,'hex'),status='matching',last_stage='matching',tmdb_id=NULL,match_payload=NULL,confirmed_at=NULL,approved_signature=NULL,lease_token=NULL,lease_expires_at=NULL,last_error=NULL,next_attempt_at=now(),updated_at=now()
 FROM tgstrm.stream_assets sa,tgstrm.movie_monitors mm WHERE sa.message_id=d.source_message_id AND mm.id=d.monitor_id AND mm.status='active'
 AND (d.status IN ('review','ready','matching','failed') OR (d.status='excluded' AND d.last_error='Archivo idéntico ya presente o reservado en este destino.')) AND d.presentation_telegram_message_id IS NULL AND d.last_stage NOT IN ('presenting','sending','indexing','exporting')
 AND (d.lease_expires_at IS NULL OR d.lease_expires_at<now()) AND d.source_signature<>encode(sa.source_signature,'hex')`)

}

type movieDelivery struct {
	id, monitor, source, group, topic, stage, lease, body, file, signature, approved string
	posterID, videoID                                                                int
	posterRandom, videoRandom                                                        int64
	payload                                                                          []byte
	destination                                                                      sql.NullString
}

func (a *API) processMovieDelivery(ctx context.Context) {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('tgstrm-movie-delivery-worker'))`).Scan(&locked); err != nil || !locked {
		return
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtext('tgstrm-movie-delivery-worker'))`)
	}()
	a.processMovieDeliveryLocked(ctx, false)
}
func (a *API) processMovieDeliveryLocked(ctx context.Context, matchingOnly bool) {
	var d movieDelivery
	err := a.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT d.id FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.movie_monitors mm ON mm.id=d.monitor_id
 WHERE mm.status='active' AND d.status NOT IN ('completed','excluded','review') AND d.next_attempt_at<=now()
 AND ((d.status='matching' OR (d.status='failed' AND d.last_stage='matching'))=$1)
 AND (d.lease_expires_at IS NULL OR d.lease_expires_at<now())
 AND (d.status='matching' OR (d.status='failed' AND d.last_stage='matching') OR NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_scopes s WHERE s.monitor_id=mm.id AND NOT s.initial_scan_complete))
 AND ($1 OR NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries other JOIN tgstrm.movie_monitors om ON om.id=other.monitor_id
 WHERE other.id<>d.id AND om.destination_chat_id=mm.destination_chat_id
 AND (other.lease_expires_at>now() OR (other.status IN ('presenting','sending') OR (other.status='failed' AND other.last_stage IN ('presenting','sending'))))))
 ORDER BY CASE WHEN d.status IN ('presenting','sending','indexing','exporting') OR d.last_stage IN ('presenting','sending','indexing','exporting') THEN 0 ELSE 1 END,(SELECT telegram_message_id FROM tgstrm.messages WHERE id=d.source_message_id),d.id
 FOR UPDATE OF d SKIP LOCKED LIMIT 1)
 UPDATE tgstrm.movie_monitor_deliveries d SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes',attempts=attempts+1
 FROM candidate c,tgstrm.movie_monitors mm,tgstrm.messages m,tgstrm.message_media media
 WHERE d.id=c.id AND mm.id=d.monitor_id AND m.id=d.source_message_id AND media.message_id=m.id
 RETURNING d.id::text,d.monitor_id::text,d.source_message_id::text,mm.destination_chat_id::text,mm.destination_topic_id::text,
 CASE WHEN d.status='failed' THEN d.last_stage ELSE d.status END,d.lease_token::text,COALESCE(m.body,''),COALESCE(media.file_name,''),d.source_signature,COALESCE(d.approved_signature,''),COALESCE(d.presentation_telegram_message_id,0),COALESCE(d.destination_telegram_message_id,0),d.presentation_random_id,d.video_random_id,d.match_payload,d.destination_message_id::text`, matchingOnly).Scan(&d.id, &d.monitor, &d.source, &d.group, &d.topic, &d.stage, &d.lease, &d.body, &d.file, &d.signature, &d.approved, &d.posterID, &d.videoID, &d.posterRandom, &d.videoRandom, &d.payload, &d.destination)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("movie delivery claim: %v", err)
		}
		return
	}
	service := a.telegramService()
	if service == nil {
		a.failMovieDelivery(ctx, d, errors.New("Telegram se está reconectando"))
		return
	}
	var match tmdbResult
	if d.stage == "matching" {
		var ok bool
		match, ok, err = a.identifyMonitoredMovie(ctx, d.body, d.file)
		if err != nil {
			a.failMovieDelivery(ctx, d, err)
			return
		}
		if !ok {
			a.finishMovieStage(ctx, d, "review", "No hay una coincidencia única de título y año.")
			return
		}
		match, err = a.tmdbMovieDetails(ctx, match.ID)
		if err != nil {
			a.failMovieDelivery(ctx, d, fmt.Errorf("TMDB no pudo consultar la ficha: %w", err))
			return
		}
		if err = validateMonitoredMovieMatch(match); err != nil {
			a.finishMovieStage(ctx, d, "review", err.Error())
			return
		}
		payload, _ := json.Marshal(match)
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET status='ready',last_stage='ready',tmdb_id=$3,match_payload=$4,confirmed_at=now(),approved_signature=source_signature,lease_token=NULL,lease_expires_at=NULL,last_error=NULL,updated_at=now() WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, match.ID, payload)
		if err == nil {
			a.publishMovieMonitor(ctx, d.monitor)
		}
		return
	}
	if err = json.Unmarshal(d.payload, &match); err != nil {
		a.failMovieDelivery(ctx, d, err)
		return
	}
	if err = validateMonitoredMovieMatch(match); err != nil {
		a.failMovieDelivery(ctx, d, err)
		return
	}
	switch d.stage {
	case "ready", "presenting":
		var current string
		var active bool
		err = a.db.QueryRowContext(ctx, `SELECT encode(sa.source_signature,'hex'),m.is_active FROM tgstrm.stream_assets sa JOIN tgstrm.messages m ON m.id=sa.message_id WHERE m.id::text=$1`, d.source).Scan(&current, &active)
		if err != nil {
			a.failMovieDelivery(ctx, d, err)
			return
		}
		if !active {
			a.finishMovieStage(ctx, d, "excluded", "El archivo origen ya no está disponible.")
			return
		}
		if d.approved != current {
			a.finishMovieStage(ctx, d, "review", "El archivo cambió después de identificarlo; revisa su coincidencia.")
			return
		}
		if !a.guardMovieDeliveryFile(ctx, d) {
			return
		}
		if _, err = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET status='presenting',last_stage='presenting' WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease); err != nil {
			a.failMovieDelivery(ctx, d, err)
			return
		}
		id, sendErr := service.SendMonitoredMovie(ctx, d.group, d.topic, d.source, movieCaption(match)+"\nhttps://www.themoviedb.org/movie/"+fmt.Sprint(match.ID), match.PosterPath, d.posterRandom, true)
		if sendErr != nil {
			d.stage = "presenting"
			a.failMovieDelivery(ctx, d, sendErr)
			return
		}
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET presentation_telegram_message_id=$3,status='sending',last_stage='sending',lease_token=NULL,lease_expires_at=NULL,last_error=NULL WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, id)
	case "sending":
		if !a.guardMovieDeliveryFile(ctx, d) {
			return
		}
		id, sendErr := service.SendMonitoredMovie(ctx, d.group, d.topic, d.source, movieCaption(match), "", d.videoRandom, false)
		if sendErr != nil {
			a.failMovieDelivery(ctx, d, sendErr)
			return
		}
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET destination_telegram_message_id=$3,status='indexing',last_stage='indexing',lease_token=NULL,lease_expires_at=NULL,last_error=NULL WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, id)
	case "indexing":
		id, indexErr := service.IndexMonitoredMovie(ctx, d.group, d.topic, d.videoID)
		if indexErr != nil {
			a.failMovieDelivery(ctx, d, indexErr)
			return
		}
		tx, txErr := a.db.BeginTx(ctx, nil)
		if txErr != nil {
			a.failMovieDelivery(ctx, d, txErr)
			return
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,overview,metadata)
 SELECT m.topic_id,m.id,'movie',$2,$3,$4,NULLIF($5,'')::date,$6,$7,$8 FROM tgstrm.messages m WHERE m.id::text=$1
 ON CONFLICT(message_id) WHERE message_id IS NOT NULL DO UPDATE SET tmdb_id=EXCLUDED.tmdb_id,title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,poster_path=EXCLUDED.poster_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,updated_at=now()`, id, match.ID, match.Title, match.OriginalTitle, match.ReleaseDate, match.PosterPath, match.Overview, d.payload)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET destination_message_id=$3::bigint,status='exporting',last_stage='exporting',lease_token=NULL,lease_expires_at=NULL,last_error=NULL WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, id)
		}
		if err == nil {
			err = tx.Commit()
		}
	case "exporting":
		if !d.destination.Valid {
			a.failMovieDelivery(ctx, d, errors.New("el vídeo destino aún no está indexado"))
			return
		}
		if code := a.generateSelectedTopicSTRM(ctx, d.group, d.topic, d.destination.String); code >= 300 {
			a.failMovieDelivery(ctx, d, fmt.Errorf("generación STRM pendiente (HTTP %d)", code))
			return
		}
		var written bool
		err = a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.stream_exports e JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id WHERE sa.message_id::text=$1 AND e.status='written')`, d.destination.String).Scan(&written)
		if err != nil || !written {
			if err == nil {
				err = errors.New("el archivo requiere revisión antes de generar STRM")
			}
			a.failMovieDelivery(ctx, d, err)
			return
		}
		a.finishMovieStage(ctx, d, "completed", "")
		return
	default:
		a.failMovieDelivery(ctx, d, fmt.Errorf("fase de películas desconocida: %s", d.stage))
		return
	}
	if err != nil {
		a.failMovieDelivery(ctx, d, err)
	} else {
		a.publishMovieMonitor(ctx, d.monitor)
	}
}
func (a *API) finishMovieStage(ctx context.Context, d movieDelivery, status, last string) {
	_, err := a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET status=$3,last_stage=$3,last_error=NULLIF($4,''),lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, status, last)
	if err == nil {
		a.publishMovieMonitor(ctx, d.monitor)
	}
}
func (a *API) failMovieDelivery(ctx context.Context, d movieDelivery, err error) {
	if d.stage == "matching" && movieTMDBResourceMissing(err) {
		a.finishMovieStage(ctx, d, "review", "La ficha o referencia de TMDB ya no está disponible. Revisa la coincidencia.")
		return
	}
	// No message was accepted after these definite preparation failures.
	// Preserve the match and retry, but let unrelated movies progress meanwhile.
	d.stage = movieFailureStage(d.stage, err)

	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET status='failed',last_stage=$3,last_error=$4,next_attempt_at=now()+$5::interval,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id::text=$1 AND lease_token::text=$2`, d.id, d.lease, d.stage, err.Error(), movieRetryDelay(err).String())
	a.publishMovieMonitor(ctx, d.monitor)
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

// Caption and filename are independent evidence. Conflicting unique matches
// require review; neither title is silently used to override the other.
func (a *API) identifyMonitoredMovieLegacy(ctx context.Context, body, file string) (tmdbResult, bool, error) {
	if monitoredMovieNeedsManualMediaReview(body, file) {
		return tmdbResult{}, false, nil
	}
	sources := monitoredMovieSources(body, file)
	var found tmdbResult
	seen := map[string]bool{}
	for _, source := range sources {
		title, year := splitTMDBTitleYear(source)
		key := normalizedTMDBTitle(source) + "|" + year
		if source == "" || len(source) > 180 || seen[key] {
			continue
		}
		seen[key] = true
		var response struct {
			Results []tmdbResult `json:"results"`
		}
		values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}}
		if year != "" {
			values.Set("primary_release_year", year)
		}
		if err := a.tmdbRequestContext(ctx, "search/movie", values, &response); err != nil {
			var provider tmdbResponseError
			if errors.As(err, &provider) {
				if provider.status == 400 || provider.status == 422 {
					continue
				}
				return tmdbResult{}, false, fmt.Errorf("TMDB no pudo buscar la película (HTTP %d): %w", provider.status, err)
			}
			return tmdbResult{}, false, errors.New("TMDB no pudo buscar la película por un fallo de conexión o tiempo de espera; se reintentará.")
		}
		if match, ok := monitoredMovieExactMatch(source, response.Results); ok {
			if found.ID > 0 && found.ID != match.ID {
				return tmdbResult{}, false, nil
			}
			found = match
		}
	}
	return found, found.ID > 0, nil
}

func movieFailureStage(stage string, err error) string {
	if stage == "presenting" && (errors.Is(err, telegramauth.ErrMoviePosterPreparation) || tgerr.Is(err, "WEBPAGE_CURL_FAILED", "PHOTO_INVALID", "PHOTO_INVALID_DIMENSIONS")) {
		return "ready"
	}
	return stage
}
