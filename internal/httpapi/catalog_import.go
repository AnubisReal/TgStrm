package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	telegramauth "tgstrm/internal/telegram"
)

type catalogImportInput struct {
	MessageIDs       []string `json:"message_ids"`
	TMDBID           int64    `json:"tmdb_id,omitempty"`
	MediaType        string   `json:"media_type,omitempty"`
	DestinationGroup string   `json:"destination_group_id,omitempty"`
	DestinationTopic string   `json:"destination_topic_id,omitempty"`
}

type catalogPreviewInput struct {
	TopicIDs     []string          `json:"topic_ids"`
	Prepare      bool              `json:"prepare,omitempty"`
	MediaTypes   map[string]string `json:"media_types,omitempty"`
	MatchQueries map[string]string `json:"match_queries,omitempty"`
}

func normalizeCatalogMediaType(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "movie") || strings.EqualFold(strings.TrimSpace(value), "pelicula") || strings.EqualFold(strings.TrimSpace(value), "película") {
		return "movie"
	}
	return "tv"
}

func (a *API) importTopicToCatalog(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if a.db == nil || service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var input catalogImportInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", "Revisa los archivos seleccionados.")
		return
	}

	groupID, topicID := r.PathValue("groupID"), r.PathValue("topicID")
	var sourceTitle string
	err := a.db.QueryRowContext(r.Context(), `SELECT t.title FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.is_active`, topicID, groupID).Scan(&sourceTitle)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "topic_not_found", "El topic externo no está disponible.")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "topic_failed", "No se pudo preparar el topic.")
		return
	}

	mediaType := normalizeCatalogMediaType(input.MediaType)
	var match tmdbResult
	var matched bool
	if mediaType == "movie" {
		match, matched = a.matchExternalMovie(r, sourceTitle)
	} else {
		match, matched = a.matchExternalSeries(r, sourceTitle)
	}
	if input.TMDBID > 0 {
		if mediaType == "movie" {
			match, err = a.localizedMovieDetails(r.Context(), input.TMDBID)
		} else {
			match, err = a.localizedTVDetails(r.Context(), input.TMDBID)
		}
		if err != nil {
			fail(w, http.StatusBadGateway, "tmdb_details_failed", "TMDB no pudo confirmar la serie elegida.")
			return
		}
		matched = true
	} else if matched && mediaType == "tv" {
		// Search considers translated, original and alternative titles. Fetching
		// the selected identity again with es-ES makes the destination name
		// independent from the title that happened to produce the match.
		if localized, detailErr := a.localizedTVDetails(r.Context(), match.ID); detailErr == nil {
			match = localized
		}
	}
	destinationTitle := strings.TrimSpace(sourceTitle)
	if matched && mediaType == "tv" {
		destinationTitle = catalogTopicTitle(match)
	}

	destinationTopicID := strings.TrimSpace(input.DestinationTopic)
	destinationGroupID, err := a.resolveManualImportDestination(r.Context(), groupID, topicID, input.DestinationGroup, destinationTopicID, mediaType, match.ID)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "invalid_destination", err.Error())
		return
	}

	syncCtx, cancelSync := context.WithTimeout(r.Context(), 5*time.Minute)
	err = service.SyncTopicVideoCatalog(syncCtx, groupID, topicID)
	cancelSync()
	if err != nil {
		fail(w, http.StatusBadGateway, "topic_sync_failed", "No se pudo completar el historial del topic antes de enviarlo.")
		return
	}

	availableVideos := 0
	foundSelected := make(map[string]bool, len(input.MessageIDs))
	availableIDs := make([]string, 0)
	rows, queryErr := a.db.QueryContext(r.Context(), `SELECT m.id::text FROM tgstrm.messages m
		WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`, topicID)
	if queryErr == nil {
		defer rows.Close()
		selected := make(map[string]bool, len(input.MessageIDs))
		for _, id := range input.MessageIDs {
			selected[id] = true
		}
		for rows.Next() {
			var messageID string
			if rows.Scan(&messageID) != nil {
				continue
			}
			availableVideos++
			availableIDs = append(availableIDs, messageID)
			if len(selected) > 0 && !selected[messageID] {
				continue
			}
			foundSelected[messageID] = true
		}
		if err := rows.Err(); err != nil {
			fail(w, http.StatusInternalServerError, "videos_failed", "No se pudo leer la selección completa de vídeos.")
			return
		}
	} else {
		fail(w, http.StatusInternalServerError, "videos_failed", "No se pudieron preparar los vídeos del topic.")
		return
	}
	if availableVideos == 0 || (len(input.MessageIDs) > 0 && len(foundSelected) != len(input.MessageIDs)) {
		fail(w, http.StatusUnprocessableEntity, "no_videos", "No hay vídeos válidos en la selección.")
		return
	}

	selectedIDs := append([]string(nil), input.MessageIDs...)
	if len(selectedIDs) == 0 {
		selectedIDs = availableIDs
	}
	importID, alreadyQueued, err := a.enqueueCatalogImportToGroup(r.Context(), topicID, destinationGroupID, destinationTopicID, destinationTitle, selectedIDs, match, matched, mediaType)
	if err != nil {
		fail(w, http.StatusConflict, "import_queue_failed", err.Error())
		return
	}
	requested := availableVideos
	requested = len(selectedIDs)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": importID, "status": "queued", "already_queued": alreadyQueued,
		"destination_title": destinationTitle, "destination_topic_id": destinationTopicID, "tmdb_matched": matched, "media_type": mediaType, "requested": requested,
	})
}

func catalogTopicTitle(match tmdbResult) string {
	title := strings.TrimSpace(match.Title)
	if title == "" {
		title = strings.TrimSpace(match.OriginalTitle)
	}
	if len(match.ReleaseDate) >= 4 {
		title += " (" + match.ReleaseDate[:4] + ")"
	}
	suffix := fmt.Sprintf(" [tmdbid-%d]", match.ID)
	const telegramTopicLimit = 128
	maxTitleRunes := telegramTopicLimit - len([]rune(suffix))
	if runes := []rune(title); len(runes) > maxTitleRunes {
		title = strings.TrimSpace(string(runes[:maxTitleRunes]))
	}
	return title + suffix
}

func (a *API) previewCatalogImports(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var input catalogPreviewInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || len(input.TopicIDs) == 0 || len(input.TopicIDs) > 50 {
		fail(w, http.StatusBadRequest, "invalid_topics", "Selecciona entre 1 y 50 topics.")
		return
	}
	if input.Prepare {
		a.startImportPreviewPreparation(r.PathValue("groupID"), input.TopicIDs)
	}
	destinations := a.importDestinations(r.Context())
	destinationGroups := a.importDestinationGroups(r.Context(), r.PathValue("groupID"))
	items := make([]map[string]any, 0, len(input.TopicIDs))
	for _, topicID := range input.TopicIDs {
		var title string
		var videoCount int
		err := a.db.QueryRowContext(r.Context(), `SELECT t.title,count(m.id) FILTER (WHERE m.is_active AND m.media_type='video')
			FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id LEFT JOIN tgstrm.messages m ON m.topic_id=t.id
			WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.is_active GROUP BY t.id`, topicID, r.PathValue("groupID")).Scan(&title, &videoCount)
		if err != nil {
			continue
		}
		matchQuery := strings.TrimSpace(input.MatchQueries[topicID])
		// A review opened from a single search result must match that file,
		// not the name of the topic which happens to contain it. Full-topic
		// imports deliberately retain the topic title as their search key.
		if len([]rune(matchQuery)) < 2 || len([]rune(matchQuery)) > 160 {
			matchQuery = title
		}
		mediaType := normalizeCatalogMediaType(input.MediaTypes[topicID])
		var results []tmdbResult
		suggestedID := int64(0)
		if input.Prepare {
			if mediaType == "movie" {
				results, _ = a.searchExternalMovies(r.Context(), matchQuery)
			} else {
				results, _ = a.searchExternalSeries(r.Context(), matchQuery)
			}
			if suggested, ok := tmdbExactMatch(matchQuery, results); ok {
				suggestedID = suggested.ID
			}
			if len(results) > 6 {
				results = results[:6]
			}
		}
		preparationStatus, preparationError := "idle", ""
		_ = a.db.QueryRowContext(r.Context(), `SELECT ss.status,COALESCE(ss.last_error,'') FROM tgstrm.sync_states ss
			JOIN tgstrm.topics t ON t.id::text=$1 JOIN tgstrm.chats c ON c.id=t.chat_id AND c.account_id=ss.account_id
			WHERE ss.scope='import_preview' AND ss.resource_id::text=$1`, topicID).Scan(&preparationStatus, &preparationError)
		items = append(items, map[string]any{"topic_id": topicID, "source_title": matchQuery, "topic_title": title, "video_count": videoCount,
			"suggested_id": suggestedID, "candidates": results, "media_type": mediaType, "destinations": destinations, "destination_groups": destinationGroups, "preparation_status": preparationStatus, "preparation_error": preparationError})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "image_base_url": "https://image.tmdb.org/t/p/w342"})
}

func (a *API) startImportPreviewPreparation(groupID string, topicIDs []string) {
	var accountID string
	if err := a.db.QueryRow(`SELECT account_id FROM tgstrm.chats WHERE id::text=$1 AND is_active`, groupID).Scan(&accountID); err != nil {
		return
	}
	key := "import-preview:" + groupID
	a.tmdbScanMu.Lock()
	if a.tmdbScans == nil {
		a.tmdbScans = make(map[string]bool)
	}
	if a.tmdbScans[key] {
		a.tmdbScanMu.Unlock()
		return
	}
	a.tmdbScans[key] = true
	a.tmdbScanMu.Unlock()
	ids := append([]string(nil), topicIDs...)
	for _, topicID := range ids {
		_, _ = a.db.Exec(`INSERT INTO tgstrm.sync_states(account_id,scope,resource_id,status,started_at,last_error)
			VALUES($1,'import_preview',$2,'running',now(),NULL) ON CONFLICT(account_id,scope,resource_id)
			DO UPDATE SET status='running',started_at=now(),completed_at=NULL,last_error=NULL,updated_at=now()`, accountID, topicID)
	}
	go func() {
		defer func() {
			a.tmdbScanMu.Lock()
			delete(a.tmdbScans, key)
			a.tmdbScanMu.Unlock()
		}()
		service := a.telegramService()
		for _, topicID := range ids {
			status, lastError := "ready", ""
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			if service == nil {
				status, lastError = "failed", "Telegram no está conectado."
			} else if err := service.SyncTopicVideoCatalog(ctx, groupID, topicID); err != nil {
				status, lastError = "failed", err.Error()
			}
			cancel()
			_, _ = a.db.Exec(`UPDATE tgstrm.sync_states SET status=$3,completed_at=now(),last_error=NULLIF($4,''),updated_at=now()
				WHERE account_id=$1 AND scope='import_preview' AND resource_id=$2`, accountID, topicID, status, fmt.Sprintf("%.500s", lastError))
			_, _ = a.db.Exec(`INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
				VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::bigint::text,'preview',true))`, topicID)
		}
	}()
}

func (a *API) catalogImportStatus(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var id, status, destinationTitle, lastError string
	var requested, completed, failed int
	err := a.db.QueryRowContext(r.Context(), `SELECT ci.id::text,ci.status,COALESCE(dt.title,ci.destination_title,''),ci.requested_count,ci.completed_count,
		count(cim.source_message_id) FILTER (WHERE cim.status='failed'),COALESCE(ci.last_error,'')
		FROM tgstrm.catalog_imports ci JOIN tgstrm.topics st ON st.id=ci.source_topic_id
		LEFT JOIN tgstrm.topics dt ON dt.id=ci.destination_topic_id
		LEFT JOIN tgstrm.catalog_import_messages cim ON cim.import_id=ci.id
		WHERE st.id::text=$1 AND st.chat_id::text=$2 GROUP BY ci.id,dt.title ORDER BY ci.updated_at DESC LIMIT 1`, r.PathValue("topicID"), r.PathValue("groupID")).
		Scan(&id, &status, &destinationTitle, &requested, &completed, &failed, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "idle"})
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "import_status_failed", "No se pudo leer el estado de importación.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "destination_title": destinationTitle, "requested": requested, "completed": completed, "failed": failed, "last_error": lastError})
}

func (a *API) runCatalogImport(parent context.Context, importID, leaseToken, groupID, topicID, destinationTopicID, destinationTitle, mediaType string, selectedIDs []string, match tmdbResult, matched bool) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	captionTitle, _, _ := splitTMDBIDMarker(destinationTitle)
	if matched {
		captionTitle = strings.TrimSpace(match.Title)
		if captionTitle == "" {
			captionTitle = strings.TrimSpace(match.OriginalTitle)
		}
		if len(match.ReleaseDate) >= 4 {
			captionTitle += " (" + match.ReleaseDate[:4] + ")"
		}
	}
	captions := make(map[string]string)
	sources, err := a.loadCatalogCaptionSources(ctx, topicID, selectedIDs)
	if err != nil {
		a.failCatalogImport(importID, topicID, leaseToken, err)
		return
	}
	// Release the result connection before episode detection makes nested DB
	// requests or network calls. A small pool must not depend on a free slot
	// while this import retains one for its entire caption preparation.
	for _, source := range sources {
		messageID, fileName, caption := source.messageID, source.fileName, source.caption
		if mediaType == "movie" && matched {
			captions[messageID] = captionTitle
		} else if episode, ok := a.detectSeriesEpisode(ctx, match.ID, fileName, caption); ok {
			episodeTitle := ""
			if episode.EndEpisode > episode.Episode {
				episodeTitle = fmt.Sprintf("Episodios %02d-%02d", episode.Episode, episode.EndEpisode)
			} else if matched {
				episodeTitle = a.tmdbEpisodeTitle(ctx, match.ID, episode.Season, episode.Episode)
			}
			normalized := captionTitle + " " + episode.code()
			if episodeTitle != "" {
				normalized += " " + episodeTitle
			}
			captions[messageID] = normalized
		} else if matched && strings.TrimSpace(caption) == "" {
			// Identify the series without inventing an unrecognized episode.
			captions[messageID] = captionTitle
		}
	}
	result, err := a.telegram.ImportQueuedTopicPreparedToTopic(ctx, importID, groupID, topicID, destinationTopicID, destinationTitle, selectedIDs, captions,
		func(prepared telegramauth.ImportResult) error {
			if err := a.persistCatalogDestination(ctx, prepared, match, matched, mediaType); err != nil {
				return err
			}
			var replication bool
			if err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_replications WHERE import_id=$1::uuid)`, importID).Scan(&replication); err != nil {
				return err
			}
			// Every matched series uses the same durable presentation step,
			// including manual imports. SendTopicPresentation is idempotent:
			// an existing presentation is reused before any videos are copied.
			// Movie collections retain their separate per-movie workflow.
			if (replication || mediaType == "tv") && matched && match.PosterPath != "" {
				return a.telegram.SendTopicPresentation(ctx, prepared.DestinationGroupID, prepared.DestinationTopicID, mediaType, match.ID, match.PosterPath)
			}
			return nil
		})
	if err != nil {
		a.failCatalogImport(importID, topicID, leaseToken, err)
		return
	}

	// Telegram confirms each send before ImportTopic returns, but realtime
	// updates can still be coalesced while a large batch is in flight. Read the
	// complete topic once here, then generate from that authoritative snapshot.
	if err = a.telegram.SyncTopicVideoCatalog(ctx, result.DestinationGroupID, result.DestinationTopicID); err != nil {
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET last_error=$2,updated_at=now() WHERE id=$1`,
			result.ImportID, fmt.Sprintf("archivos enviados; indexación pendiente: %.400s", err.Error()))
		return
	}
	var catalogueEnabled bool
	if err = a.db.QueryRowContext(ctx, `SELECT catalogue_enabled FROM tgstrm.catalog_imports WHERE id::text=$1`, result.ImportID).Scan(&catalogueEnabled); err != nil {
		return
	}
	if !catalogueEnabled {
		return
	}
	if mediaType == "movie" {
		// The review already contains the exact TMDB identity selected by the
		// operator. Persist it on the copied destination files immediately;
		// relying only on the asynchronous scanner left a window where a movie
		// could be marked unresolved before that identity became available.
		if matched {
			if err = a.persistImportedMovieMatches(ctx, topicID, result.DestinationTopicID, selectedIDs, match); err != nil {
				_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET last_error=$2,updated_at=now() WHERE id=$1`,
					result.ImportID, fmt.Sprintf("archivos enviados; coincidencia TMDB pendiente: %.400s", err.Error()))
			}
		}
		// Keep scanning the collection for unrelated files whose identity was
		// not chosen during an import review.
		a.runTMDBMovieScan(ctx, result.DestinationGroupID, result.DestinationTopicID)
	}
	status := a.generateAutomaticTopicSTRM(ctx, result.DestinationGroupID, result.DestinationTopicID)
	if status < 200 || status >= 300 {
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET last_error=$2,updated_at=now() WHERE id=$1`,
			result.ImportID, fmt.Sprintf("archivos indexados; exportación STRM pendiente (HTTP %d)", status))
	}
}

// persistImportedMovieMatches transfers the confirmed TMDB identity selected
// in the review to the exact documents accepted by Telegram. Telegram keeps a
// document's ID when it is re-sent, which gives us a durable, content-level
// join without depending on message ordering or a later UI refresh.
func (a *API) persistImportedMovieMatches(ctx context.Context, sourceTopicID, destinationTopicID string, sourceMessageIDs []string, match tmdbResult) error {
	if len(sourceMessageIDs) == 0 || match.ID <= 0 || strings.TrimSpace(destinationTopicID) == "" {
		return nil
	}
	metadata, err := json.Marshal(match)
	if err != nil {
		return err
	}
	placeholders := make([]string, 0, len(sourceMessageIDs))
	args := []any{destinationTopicID, match.ID, match.Title, match.OriginalTitle, match.ReleaseDate, match.PosterPath, match.Overview, metadata, sourceTopicID}
	for _, messageID := range sourceMessageIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d::bigint", len(args)+1))
		args = append(args, messageID)
	}
	query := fmt.Sprintf(`INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,overview,metadata)
		SELECT destination.topic_id,destination.id,'movie',$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,''),$7,$8
		FROM tgstrm.messages source
		JOIN tgstrm.message_media source_media ON source_media.message_id=source.id
		JOIN tgstrm.messages destination ON destination.topic_id=$1::bigint AND destination.is_active
		JOIN tgstrm.message_media destination_media ON destination_media.message_id=destination.id
			AND destination_media.telegram_file_id=source_media.telegram_file_id
		WHERE source.topic_id=$9::bigint AND source.id IN (%s)
		ON CONFLICT(message_id) WHERE message_id IS NOT NULL DO UPDATE SET media_type='movie',tmdb_id=EXCLUDED.tmdb_id,
			title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,
			poster_path=EXCLUDED.poster_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,
			confirmed_at=now(),updated_at=now()`, strings.Join(placeholders, ","))
	_, err = a.db.ExecContext(ctx, query, args...)
	return err
}

func (a *API) persistCatalogDestination(ctx context.Context, result telegramauth.ImportResult, match tmdbResult, matched bool, mediaType string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	kind := "series"
	if mediaType == "movie" {
		kind = "movie_collection"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.topics SET library_kind=$3,updated_at=now()
		WHERE id::text=$1 AND chat_id::text=$2`, result.DestinationTopicID, result.DestinationGroupID, kind); err != nil {
		return err
	}
	if matched {
		metadata, _ := json.Marshal(match)
		if mediaType == "movie" {
			if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET tmdb_media_type='movie',tmdb_id=$2,updated_at=now() WHERE id=$1`, result.ImportID, match.ID); err != nil {
				return err
			}
		} else if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,backdrop_path,overview,metadata)
			VALUES($1,NULL,'tv',$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,''),NULLIF($7,''),$8,$9)
			ON CONFLICT(topic_id) WHERE message_id IS NULL DO UPDATE SET media_type='tv',tmdb_id=EXCLUDED.tmdb_id,
			title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,
			poster_path=EXCLUDED.poster_path,backdrop_path=EXCLUDED.backdrop_path,overview=EXCLUDED.overview,
			metadata=EXCLUDED.metadata,confirmed_at=now(),updated_at=now()`, result.DestinationTopicID, match.ID,
			match.Title, match.OriginalTitle, match.ReleaseDate, match.PosterPath, match.BackdropPath, match.Overview, metadata); err != nil {
			return err
		}
		if mediaType != "movie" {
			if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET tmdb_media_type='tv',tmdb_id=$2,updated_at=now() WHERE id=$1`, result.ImportID, match.ID); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','catalog_destination_ready'))`, result.DestinationGroupID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *API) localizedTVDetails(ctx context.Context, tmdbID int64) (tmdbResult, error) {
	var details struct {
		ID           int64   `json:"id"`
		Name         string  `json:"name"`
		OriginalName string  `json:"original_name"`
		FirstAirDate string  `json:"first_air_date"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		Overview     string  `json:"overview"`
		Popularity   float64 `json:"popularity"`
	}
	if err := a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d", tmdbID), url.Values{"language": {"es-ES"}}, &details); err != nil {
		return tmdbResult{}, err
	}
	return tmdbResult{ID: details.ID, Title: details.Name, OriginalTitle: details.OriginalName,
		ReleaseDate: details.FirstAirDate, PosterPath: details.PosterPath, BackdropPath: details.BackdropPath,
		Overview: details.Overview, Popularity: details.Popularity}, nil
}

func (a *API) matchExternalSeries(r *http.Request, source string) (tmdbResult, bool) {
	results, err := a.searchExternalSeries(r.Context(), source)
	if err != nil {
		return tmdbResult{}, false
	}
	return tmdbExactMatch(source, results)
}

func (a *API) searchExternalSeries(ctx context.Context, source string) ([]tmdbResult, error) {
	var response struct {
		Results []struct {
			ID           int64   `json:"id"`
			Name         string  `json:"name"`
			OriginalName string  `json:"original_name"`
			FirstAirDate string  `json:"first_air_date"`
			PosterPath   string  `json:"poster_path"`
			Overview     string  `json:"overview"`
			Popularity   float64 `json:"popularity"`
		} `json:"results"`
	}
	title, year := splitTMDBTitleYear(source)
	values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}, "page": {"1"}}
	if year != "" {
		values.Set("first_air_date_year", year)
	}
	if err := a.tmdbRequestContext(ctx, "search/tv", values, &response); err != nil {
		return nil, err
	}
	results := make([]tmdbResult, 0, len(response.Results))
	for _, item := range response.Results {
		results = append(results, tmdbResult{ID: item.ID, Title: item.Name, OriginalTitle: item.OriginalName,
			ReleaseDate: item.FirstAirDate, PosterPath: item.PosterPath, Overview: item.Overview, Popularity: item.Popularity})
	}
	return results, nil
}

func (a *API) searchExternalMovies(ctx context.Context, source string) ([]tmdbResult, error) {
	var response struct {
		Results []struct {
			ID            int64   `json:"id"`
			Title         string  `json:"title"`
			OriginalTitle string  `json:"original_title"`
			ReleaseDate   string  `json:"release_date"`
			PosterPath    string  `json:"poster_path"`
			BackdropPath  string  `json:"backdrop_path"`
			Overview      string  `json:"overview"`
			Popularity    float64 `json:"popularity"`
		} `json:"results"`
	}
	title, year := splitTMDBTitleYear(source)
	values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}, "page": {"1"}}
	if year != "" {
		values.Set("primary_release_year", year)
	}
	if err := a.tmdbRequestContext(ctx, "search/movie", values, &response); err != nil {
		return nil, err
	}
	results := make([]tmdbResult, 0, len(response.Results))
	for _, item := range response.Results {
		results = append(results, tmdbResult{ID: item.ID, Title: item.Title, OriginalTitle: item.OriginalTitle,
			ReleaseDate: item.ReleaseDate, PosterPath: item.PosterPath, BackdropPath: item.BackdropPath,
			Overview: item.Overview, Popularity: item.Popularity})
	}
	return results, nil
}

func (a *API) localizedMovieDetails(ctx context.Context, tmdbID int64) (tmdbResult, error) {
	var details struct {
		ID            int64   `json:"id"`
		Title         string  `json:"title"`
		OriginalTitle string  `json:"original_title"`
		ReleaseDate   string  `json:"release_date"`
		PosterPath    string  `json:"poster_path"`
		BackdropPath  string  `json:"backdrop_path"`
		Overview      string  `json:"overview"`
		Popularity    float64 `json:"popularity"`
	}
	if err := a.tmdbRequestContext(ctx, fmt.Sprintf("movie/%d", tmdbID), url.Values{"language": {"es-ES"}}, &details); err != nil {
		return tmdbResult{}, err
	}
	return tmdbResult{ID: details.ID, Title: details.Title, OriginalTitle: details.OriginalTitle,
		ReleaseDate: details.ReleaseDate, PosterPath: details.PosterPath, BackdropPath: details.BackdropPath,
		Overview: details.Overview, Popularity: details.Popularity}, nil
}

func (a *API) matchExternalMovie(r *http.Request, source string) (tmdbResult, bool) {
	results, err := a.searchExternalMovies(r.Context(), source)
	if err != nil {
		return tmdbResult{}, false
	}
	return tmdbExactMatch(source, results)
}

func (a *API) importDestinations(ctx context.Context) []map[string]any {
	rows, err := a.db.QueryContext(ctx, `SELECT t.id::text,t.title,t.library_kind,c.id::text,c.title,COALESCE(tm.tmdb_id,0)
		FROM tgstrm.active_catalog_chats c JOIN tgstrm.topics t ON t.chat_id=c.id
		LEFT JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL
		WHERE c.is_active AND c.is_forum AND t.is_active ORDER BY lower(t.title),t.id`)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, title, kind, groupID, groupTitle string
		var tmdbID int64
		if rows.Scan(&id, &title, &kind, &groupID, &groupTitle, &tmdbID) == nil {
			items = append(items, map[string]any{"id": id, "title": title, "library_kind": kind, "group_id": groupID, "group_title": groupTitle, "tmdb_id": tmdbID})
		}
	}
	return items
}
