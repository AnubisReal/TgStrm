package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func correctedSeriesCaption(title string, episode episodeIdentity, identified bool, episodeTitle string) string {
	if !identified {
		return title
	}
	text := title + " " + episode.code()
	if episode.EndEpisode > episode.Episode {
		episodeTitle = fmt.Sprintf("Episodios %02d-%02d", episode.Episode, episode.EndEpisode)
	}
	if strings.TrimSpace(episodeTitle) != "" {
		text += " " + strings.TrimSpace(episodeTitle)
	}
	return text
}

// Prepare once per revision. Nothing becomes visible to workers until commit.
func (a *API) prepareTopicCaptionCorrections(ctx context.Context, id string, revision, tmdbID int64) error {
	var title, year string
	if err := a.db.QueryRowContext(ctx, `SELECT title,COALESCE(to_char(release_date,'YYYY'),'') FROM tgstrm.tmdb_matches WHERE topic_id::text=$1 AND message_id IS NULL AND tmdb_id=$2`, id, tmdbID).Scan(&title, &year); err != nil {
		return err
	}
	if year != "" {
		title += " (" + year + ")"
	}
	rows, err := a.db.QueryContext(ctx, `SELECT m.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,''),source.body,
 COALESCE(eo.season_number,0),COALESCE(eo.episode_number,0)
 FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
 LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
 LEFT JOIN tgstrm.stream_episode_overrides eo ON eo.asset_id=sa.id AND eo.series_tmdb_id=$2
 LEFT JOIN LATERAL (SELECT sm.body FROM tgstrm.mirror_deliveries md JOIN tgstrm.messages sm ON sm.id=md.source_message_id WHERE md.destination_message_id=m.id ORDER BY md.updated_at DESC LIMIT 1) source ON true
 WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`, id, tmdbID)
	if err != nil {
		return err
	}
	type item struct {
		id, file, previous, desired string
		source                      sql.NullString
		season, episode             int
	}
	var items []item
	for rows.Next() {
		var it item
		if err = rows.Scan(&it.id, &it.file, &it.previous, &it.source, &it.season, &it.episode); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	singleSeason := 0
	var singleTitles map[int]string
	for _, it := range items {
		caption := it.previous
		if it.source.Valid {
			caption = it.source.String
		}
		if _, identified := detectEpisode(it.file, caption); !identified {
			if _, _, possible := seasonlessEpisodeNumber(it.file, caption); possible {
				singleSeason, singleTitles, err = a.singleSeasonEpisodeTitles(ctx, tmdbID)
				if err != nil {
					return err
				}
				break
			}
		}
	}
	for i := range items {
		it := &items[i]
		caption := it.previous
		if it.source.Valid {
			caption = it.source.String
		}
		episode, identified := a.detectSeriesEpisode(ctx, tmdbID, it.file, caption)
		if it.episode > 0 {
			episode = episodeIdentity{Season: it.season, Episode: it.episode, Source: "manual"}
			identified = true
		}
		if !identified && singleSeason > 0 {
			if number, source, possible := seasonlessEpisodeNumber(it.file, caption); possible {
				if _, exists := singleTitles[number]; exists {
					episode = episodeIdentity{Season: singleSeason, Episode: number, Source: source}
					identified = true
				}
			}
		}
		if !identified && episode.Source == "context_unavailable" {
			return errors.New("No se pudo verificar la temporada en TMDB; se conserva el pie y se reintentará")
		}
		episodeTitle := ""
		if identified && episode.EndEpisode <= episode.Episode {
			if episode.Season == singleSeason && singleTitles != nil {
				episodeTitle = singleTitles[episode.Episode]
			} else {
				episodeTitle = a.tmdbEpisodeTitle(ctx, tmdbID, episode.Season, episode.Episode)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if identified {
			it.desired = correctedSeriesCaption(title, episode, true, episodeTitle)
		} else {
			// No evidence for an episode is not evidence that its text is useless.
			it.desired = it.previous
		}
	}
	for _, it := range items {
		caption := it.previous
		if it.source.Valid {
			caption = it.source.String
		}
		episode, identified := a.detectSeriesEpisode(ctx, tmdbID, it.file, caption)
		if it.episode > 0 {
			episode = episodeIdentity{Season: it.season, Episode: it.episode, Source: "manual"}
			identified = true
		}
		contextEpisode, suspect, _ := a.detectCumulativeEpisode(ctx, tmdbID, it.file)
		if contextEpisode.Source == "context_unavailable" {
			return errors.New("No se pudo verificar la temporada; se conserva el STRM y se reintentará")
		}
		if err = a.archiveIncorrectEpisodeExports(ctx, it.id, episode, identified, suspect); err != nil {
			return err
		}
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		return err
	}
	if current != revision {
		return errors.New("la coincidencia cambió durante la preparación")
	}
	for _, it := range items {
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.topic_caption_corrections(topic_id,message_id,revision,previous_caption,desired_caption,source_caption_snapshot,status)
  VALUES($1::bigint,$2::bigint,$3,$4,$5,$6,CASE WHEN $4::text=$5::text THEN 'completed' ELSE 'pending' END) ON CONFLICT(topic_id,message_id) DO UPDATE SET revision=EXCLUDED.revision,previous_caption=EXCLUDED.previous_caption,desired_caption=EXCLUDED.desired_caption,source_caption_snapshot=EXCLUDED.source_caption_snapshot,status=EXCLUDED.status,last_error=NULL,next_attempt_at=now(),updated_at=now()`, id, it.id, revision, it.previous, it.desired, it.source)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.topic_identity_jobs SET captions_prepared_revision=$2 WHERE topic_id::text=$1 AND revision=$2`, id, revision)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *API) applyOneTopicCaptionCorrection(ctx context.Context, id string, revision int64) (bool, error) {
	var message, previous, desired string
	var source sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT cc.message_id::text,cc.previous_caption,cc.desired_caption,cc.source_caption_snapshot
 FROM tgstrm.topic_caption_corrections cc JOIN tgstrm.messages m ON m.id=cc.message_id
 WHERE cc.topic_id::text=$1 AND cc.revision=$2 AND cc.status='pending' AND cc.next_attempt_at<=now() AND m.is_active
 ORDER BY m.telegram_message_id LIMIT 1`, id, revision).Scan(&message, &previous, &desired, &source)
	if errors.Is(err, sql.ErrNoRows) {
		var unfinished bool
		if err = a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_caption_corrections cc JOIN tgstrm.messages m ON m.id=cc.message_id WHERE cc.topic_id::text=$1 AND cc.revision=$2 AND cc.status<>'completed' AND m.is_active)`, id, revision).Scan(&unfinished); err != nil {
			return false, err
		}
		return !unfinished, nil
	}
	if err != nil {
		return false, err
	}
	if err = a.telegram.CorrectReplicatedVideoCaption(ctx, id, message, previous, desired); err != nil {
		return false, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		return false, err
	}
	if current != revision {
		return false, errors.New("la coincidencia cambió durante la edición")
	}
	// Acknowledge the remote edit before exposing its caption to realtime clients.
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.messages SET body=$2,updated_at=now() WHERE id::text=$1 AND is_active`, message, desired); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET last_applied_caption=$2,
 desired_caption=CASE WHEN COALESCE(source.body,'') IS NOT DISTINCT FROM $3::text THEN NULL ELSE md.desired_caption END,
 edit_status=CASE WHEN COALESCE(source.body,'') IS NOT DISTINCT FROM $3::text THEN 'idle' ELSE md.edit_status END,
 updated_at=now()
 FROM tgstrm.messages source WHERE md.destination_message_id::text=$1 AND source.id=md.source_message_id`, message, desired, source); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET normalized_caption=$2,updated_at=now() WHERE destination_message_id::text=$1`, message, desired); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.topic_caption_corrections SET status='completed',last_error=NULL,updated_at=now() WHERE topic_id::text=$1 AND message_id::text=$2 AND revision=$3`, id, message, revision); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.identity.progress','topic',$1::bigint,jsonb_build_object('reason','caption_corrected','message_id',$2::text))`, id, message); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (a *API) advanceTopicCorrection(ctx context.Context, id, previous, desired, poster string, revision, tmdbID, identityRevision, captionRevision int64) (bool, error) {
	if identityRevision != revision {
		if err := a.telegram.CorrectReplicatedTopicIdentity(ctx, id, previous, desired, poster, tmdbID); err != nil {
			return false, err
		}
		_, err := a.db.ExecContext(ctx, `UPDATE tgstrm.topic_identity_jobs SET identity_applied_revision=$2 WHERE topic_id::text=$1 AND revision=$2`, id, revision)
		return false, err
	}
	if captionRevision != revision {
		return false, a.prepareTopicCaptionCorrections(ctx, id, revision, tmdbID)
	}
	done, err := a.applyOneTopicCaptionCorrection(ctx, id, revision)
	if err != nil || !done {
		return false, err
	}
	var groupID string
	if err = a.db.QueryRowContext(ctx, `SELECT chat_id::text FROM tgstrm.topics WHERE id::text=$1`, id).Scan(&groupID); err != nil {
		return false, err
	}
	if code := a.generateAutomaticTopicSTRM(ctx, groupID, id); code != http.StatusOK {
		return false, fmt.Errorf("no se pudo regenerar la biblioteca (HTTP %d)", code)
	}
	a.retireExportPaths(ctx)
	var retiring bool
	if err = a.db.QueryRowContext(ctx, pendingTopicExportRetirementSQL, id).Scan(&retiring); err != nil {
		return false, err
	}
	return !retiring, nil
}

func (a *API) topicCorrectionStatus(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var replicated bool
	if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.topics t ON t.id=tr.destination_topic_id WHERE t.id::text=$1 AND t.chat_id::text=$2 AND t.is_active)`, r.PathValue("topicID"), r.PathValue("groupID")).Scan(&replicated); err != nil {
		fail(w, 500, "correction_status_failed", "No se pudo consultar la corrección.")
		return
	}
	var status, title, detail string
	var revision, identity, prepared int64
	var done, total int
	err := a.db.QueryRowContext(r.Context(), `SELECT j.status,j.desired_title,COALESCE(j.last_error,''),j.revision,j.identity_applied_revision,j.captions_prepared_revision,
 (SELECT count(*) FROM tgstrm.topic_caption_corrections cc JOIN tgstrm.messages m ON m.id=cc.message_id AND m.is_active WHERE cc.topic_id=j.topic_id AND cc.revision=j.revision AND cc.status='completed'),
 (SELECT count(*) FROM tgstrm.topic_caption_corrections cc JOIN tgstrm.messages m ON m.id=cc.message_id AND m.is_active WHERE cc.topic_id=j.topic_id AND cc.revision=j.revision)
 FROM tgstrm.topic_identity_jobs j JOIN tgstrm.topics t ON t.id=j.topic_id
 WHERE j.topic_id::text=$1 AND t.chat_id::text=$2`, r.PathValue("topicID"), r.PathValue("groupID")).Scan(&status, &title, &detail, &revision, &identity, &prepared, &done, &total)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"status": "idle", "replicated": replicated})
		return
	}
	if err != nil {
		fail(w, 500, "correction_status_failed", "No se pudo consultar la corrección.")
		return
	}
	phase := "pies"
	if identity != revision {
		phase = "topic y póster"
	} else if prepared != revision {
		phase = "preparando pies"
	} else if done == total {
		phase = "biblioteca"
	}
	writeJSON(w, 200, map[string]any{"status": status, "title": title, "detail": detail, "phase": phase, "completed": done, "total": total, "revision": strconv.FormatInt(revision, 10), "replicated": replicated})
}

// Recovery copies are intentionally retained and cannot block job completion.
const pendingTopicExportRetirementSQL = `SELECT EXISTS(SELECT 1 FROM tgstrm.retired_export_paths rp JOIN tgstrm.stream_assets sa ON sa.id=rp.asset_id JOIN tgstrm.messages m ON m.id=sa.message_id WHERE m.topic_id::text=$1 AND rp.relative_path NOT LIKE '.tgstrm-recovery/%')`
