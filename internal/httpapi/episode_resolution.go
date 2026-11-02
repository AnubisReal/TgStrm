package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type episodeResolutionInput struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

type episodeOption struct {
	Number    int    `json:"number"`
	Name      string `json:"name"`
	AirDate   string `json:"air_date,omitempty"`
	Overview  string `json:"overview,omitempty"`
	StillPath string `json:"still_path,omitempty"`
}

type episodeResolutionScope struct {
	AssetID, FileName, Caption, SeriesTitle string
	SeriesTMDBID                            int64
}

func (a *API) episodeResolutionScope(r *http.Request) (episodeResolutionScope, error) {
	var scope episodeResolutionScope
	err := a.db.QueryRowContext(r.Context(), `SELECT sa.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,''),tm.tmdb_id,tm.title
		FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
		JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.library_kind='series' AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL AND tm.media_type='tv'
		WHERE c.id::text=$1 AND t.id::text=$2 AND m.id::text=$3 AND m.is_active AND m.media_type='video'`,
		r.PathValue("groupID"), r.PathValue("topicID"), r.PathValue("videoID")).
		Scan(&scope.AssetID, &scope.FileName, &scope.Caption, &scope.SeriesTMDBID, &scope.SeriesTitle)
	return scope, err
}

func (a *API) episodeResolution(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	scope, err := a.episodeResolutionScope(r)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusUnprocessableEntity, "series_match_required", "Confirma primero la serie correspondiente en TMDB.")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "resolution_failed", "No se pudo preparar la resolución del episodio.")
		return
	}
	var saved struct {
		Season, Episode int
		Title           string
	}
	hasSaved := a.db.QueryRowContext(r.Context(), `SELECT season_number,episode_number,episode_title
		FROM tgstrm.stream_episode_overrides WHERE asset_id=$1`, scope.AssetID).
		Scan(&saved.Season, &saved.Episode, &saved.Title) == nil
	suggested, detected := a.detectSeriesEpisode(r.Context(), scope.SeriesTMDBID, scope.FileName, scope.Caption)
	writeJSON(w, http.StatusOK, map[string]any{
		"series":    map[string]any{"tmdb_id": scope.SeriesTMDBID, "title": scope.SeriesTitle},
		"saved":     map[string]any{"exists": hasSaved, "season": saved.Season, "episode": saved.Episode, "title": saved.Title},
		"suggested": map[string]any{"detected": detected, "season": suggested.Season, "episode": suggested.Episode},
	})
}

func (a *API) seasonEpisodes(w http.ResponseWriter, r *http.Request) {
	scope, err := a.episodeResolutionScope(r)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "series_match_required", "Confirma primero la serie correspondiente en TMDB.")
		return
	}
	season, err := strconv.Atoi(r.URL.Query().Get("season"))
	if err != nil || season < 0 || season > 99 {
		fail(w, http.StatusUnprocessableEntity, "invalid_season", "Indica una temporada válida.")
		return
	}
	var response struct {
		Episodes []struct {
			EpisodeNumber int    `json:"episode_number"`
			Name          string `json:"name"`
			AirDate       string `json:"air_date"`
			Overview      string `json:"overview"`
			StillPath     string `json:"still_path"`
		} `json:"episodes"`
	}
	path := fmt.Sprintf("tv/%d/season/%d", scope.SeriesTMDBID, season)
	if err = a.tmdbRequest(r, path, url.Values{"language": {"es-ES"}}, &response); err != nil {
		fail(w, http.StatusBadGateway, "tmdb_season_failed", "TMDB no pudo cargar esa temporada.")
		return
	}
	items := make([]episodeOption, 0, len(response.Episodes))
	for _, item := range response.Episodes {
		items = append(items, episodeOption{Number: item.EpisodeNumber, Name: item.Name, AirDate: item.AirDate, Overview: item.Overview, StillPath: item.StillPath})
	}
	writeJSON(w, http.StatusOK, map[string]any{"season": season, "episodes": items, "image_base_url": "https://image.tmdb.org/t/p/w300"})
}

func (a *API) saveEpisodeResolution(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var input episodeResolutionInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Season < 0 || input.Season > 99 || input.Episode < 1 || input.Episode > 999 {
		fail(w, http.StatusBadRequest, "invalid_episode", "Selecciona una temporada y un episodio válidos.")
		return
	}
	scope, err := a.episodeResolutionScope(r)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "series_match_required", "Confirma primero la serie correspondiente en TMDB.")
		return
	}
	var details struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("tv/%d/season/%d/episode/%d", scope.SeriesTMDBID, input.Season, input.Episode)
	if err = a.tmdbRequest(r, path, url.Values{"language": {"es-ES"}}, &details); err != nil || strings.TrimSpace(details.Name) == "" {
		fail(w, http.StatusUnprocessableEntity, "tmdb_episode_not_found", "TMDB no reconoce ese episodio para la serie seleccionada.")
		return
	}

	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, "resolution_failed", "No se pudo iniciar la asignación.")
		return
	}
	defer tx.Rollback()
	var unlocked bool
	if err = tx.QueryRowContext(r.Context(), `SELECT pg_try_advisory_xact_lock(hashtextextended('topic-identity:' || $1,0))`, r.PathValue("topicID")).Scan(&unlocked); err != nil || !unlocked {
		fail(w, http.StatusConflict, "correction_in_progress", "Se está aplicando una corrección al topic. Inténtalo de nuevo en unos segundos.")
		return
	}
	var duplicate bool
	if err = tx.QueryRowContext(r.Context(), episodeAssignmentConflictSQL, r.PathValue("topicID"), scope.AssetID, input.Season, input.Episode, scope.SeriesTMDBID).Scan(&duplicate); err != nil {
		fail(w, http.StatusInternalServerError, "resolution_failed", "No se pudo validar la asignación.")
		return
	}
	if duplicate {
		fail(w, http.StatusConflict, "episode_already_assigned", "Ese episodio ya está asignado a otro archivo del topic.")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.stream_episode_overrides(asset_id,series_tmdb_id,season_number,episode_number,episode_title)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(asset_id) DO UPDATE SET series_tmdb_id=EXCLUDED.series_tmdb_id,
		season_number=EXCLUDED.season_number,episode_number=EXCLUDED.episode_number,episode_title=EXCLUDED.episode_title,updated_at=now()`,
		scope.AssetID, scope.SeriesTMDBID, input.Season, input.Episode, details.Name)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=NULL,processed_at=NULL,last_export_error=NULL,updated_at=now() WHERE id=$1`, scope.AssetID)
	}
	if err == nil {
		var captionTitle string
		err = tx.QueryRowContext(r.Context(), `SELECT title || CASE WHEN release_date IS NOT NULL THEN ' (' || extract(year FROM release_date)::text || ')' ELSE '' END FROM tgstrm.tmdb_matches WHERE topic_id::text=$1 AND message_id IS NULL AND tmdb_id=$2`, r.PathValue("topicID"), scope.SeriesTMDBID).Scan(&captionTitle)
		if err == nil {
			caption := correctedSeriesCaption(captionTitle, episodeIdentity{Season: input.Season, Episode: input.Episode}, true, details.Name)
			_, err = tx.ExecContext(r.Context(), manualEpisodeCaptionSQL, r.PathValue("topicID"), r.PathValue("videoID"), scope.Caption, caption, scope.SeriesTMDBID)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_identity_jobs SET status='pending',next_attempt_at=now(),updated_at=now() WHERE topic_id::text=$1 AND tmdb_id=$2 AND status='completed'`, r.PathValue("topicID"), scope.SeriesTMDBID)
			}
		}
	}
	if err != nil || tx.Commit() != nil {
		fail(w, http.StatusInternalServerError, "resolution_failed", "No se pudo guardar la asignación.")
		return
	}
	status := a.generateSelectedTopicSTRM(r.Context(), r.PathValue("groupID"), r.PathValue("topicID"), r.PathValue("videoID"))
	if status < 200 || status >= 300 {
		fail(w, http.StatusInternalServerError, "export_failed", "La asignación se guardó, pero el STRM no pudo generarse.")
		return
	}
	var relative string
	_ = a.db.QueryRowContext(r.Context(), `SELECT e.relative_path FROM tgstrm.stream_exports e
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id WHERE sa.id=$1 AND e.status='written' ORDER BY e.updated_at DESC LIMIT 1`, scope.AssetID).Scan(&relative)
	writeJSON(w, http.StatusOK, map[string]any{"season": input.Season, "episode": input.Episode, "title": details.Name, "relative_path": relative})
}

const manualEpisodeCaptionSQL = `WITH ensured_job AS (
 INSERT INTO tgstrm.topic_identity_jobs(topic_id,desired_title,previous_title,tmdb_id,poster_path,identity_applied_revision,captions_prepared_revision)
 SELECT t.id,t.title,t.title,tm.tmdb_id,COALESCE(tm.poster_path,''),1,1
 FROM tgstrm.topics t JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL AND tm.media_type='tv'
 WHERE t.id::text=$1 AND tm.tmdb_id=$5
 ON CONFLICT(topic_id) DO UPDATE SET updated_at=tgstrm.topic_identity_jobs.updated_at
 RETURNING topic_id,revision,tmdb_id
) INSERT INTO tgstrm.topic_caption_corrections(topic_id,message_id,revision,previous_caption,desired_caption,source_caption_snapshot)
    SELECT j.topic_id,$2::bigint,j.revision,$3,$4,(SELECT source.body FROM tgstrm.mirror_deliveries md JOIN tgstrm.messages source ON source.id=md.source_message_id WHERE md.destination_message_id=$2::bigint ORDER BY md.updated_at DESC LIMIT 1)
    FROM ensured_job j WHERE j.topic_id::text=$1 AND j.tmdb_id=$5
    ON CONFLICT(topic_id,message_id) DO UPDATE SET revision=EXCLUDED.revision,previous_caption=EXCLUDED.previous_caption,desired_caption=EXCLUDED.desired_caption,source_caption_snapshot=EXCLUDED.source_caption_snapshot,status='pending',last_error=NULL,next_attempt_at=now(),updated_at=now()`

// Executed while holding the same topic lock as assignment and rematching.
const episodeAssignmentConflictSQL = `SELECT EXISTS(SELECT 1 FROM tgstrm.stream_episode_overrides eo
 JOIN tgstrm.stream_assets other_sa ON other_sa.id=eo.asset_id
 JOIN tgstrm.messages other_m ON other_m.id=other_sa.message_id AND other_m.is_active
 WHERE other_m.topic_id=$1::bigint AND eo.asset_id<>$2::uuid
 AND eo.season_number=$3 AND eo.episode_number=$4 AND eo.series_tmdb_id=$5)`
