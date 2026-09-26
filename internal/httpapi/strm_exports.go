package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

type strmExportInput struct {
	MediaType     string   `json:"media_type"`
	MessageIDs    []string `json:"message_ids"`
	PublicBaseURL string   `json:"public_base_url"`
}

type exportCandidate struct {
	MessageID, AssetID, FileName, Caption string
	TelegramID                            int
	OverrideSeason, OverrideEpisode       int
	OverrideTitle                         string
	MovieTitle, MovieYear                 string
	MovieTMDBID                           int64
	SentAt                                time.Time
}

type staleExport struct {
	ID, RootPath, RelativePath string
}

func safeLibraryName(value, fallback string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	value = strings.Trim(strings.TrimSpace(b.String()), ".")
	if value == "" {
		value = fallback
	}
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}

func movieSTRMPath(title, year string) (string, bool) {
	title, year = strings.TrimSpace(title), strings.TrimSpace(year)
	if title == "" || year == "" {
		return "", false
	}
	label := safeLibraryName(fmt.Sprintf("%s (%s)", title, year), "Película")
	return filepath.Join(label, label+".strm"), true
}

func shouldReplaceEpisode(startedAt, incomingAt time.Time, incomingID, existingID int) bool {
	return !startedAt.IsZero() && !incomingAt.Before(startedAt) && incomingID > existingID
}

func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tgstrm-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err = temporary.Write(content); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func removeEmptyExportParents(path, root string) {
	root = filepath.Clean(root)
	for dir := filepath.Dir(path); dir != root && strings.HasPrefix(dir, root+string(os.PathSeparator)); dir = filepath.Dir(dir) {
		removeExportDirectoryMetadata(dir)
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

// Finder and Windows Explorer may leave metadata in an otherwise empty media
// directory. These names never contain library content and are safe to remove;
// any other entry prevents the parent directory from being deleted.
func removeExportDirectoryMetadata(dir string) {
	for _, name := range []string{".DS_Store", "Thumbs.db"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() {
			_ = os.Remove(path)
		}
	}
}

func (a *API) removeStaleAssetExports(ctx context.Context, assetID, mediaType string) {
	rows, err := a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path
	 FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id
	 WHERE e.asset_id=$1 AND lt.media_type<>$2`, assetID, mediaType)
	if err != nil {
		return
	}
	var stale []staleExport
	for rows.Next() {
		var item staleExport
		if rows.Scan(&item.ID, &item.RootPath, &item.RelativePath) == nil {
			stale = append(stale, item)
		}
	}
	rows.Close()
	for _, item := range stale {
		root := filepath.Clean(item.RootPath)
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(item.RelativePath)))
		if path == root || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if result, err := a.db.ExecContext(ctx, `DELETE FROM tgstrm.stream_exports WHERE id=$1`, item.ID); err == nil {
			if deleted, _ := result.RowsAffected(); deleted > 0 {
				removeEmptyExportParents(path, root)
			}
		}
	}
}

// removeAssetExportsForMediaType retires a previously generated entry when it
// can no longer be named safely. The filesystem is changed before its durable
// row is removed, so a failed removal remains visible and can be retried.
func (a *API) removeAssetExportsForMediaType(ctx context.Context, assetID, mediaType string) {
	rows, err := a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path
	 FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id
	 WHERE e.asset_id=$1 AND lt.media_type=$2`, assetID, mediaType)
	if err != nil {
		return
	}
	var exports []staleExport
	for rows.Next() {
		var item staleExport
		if rows.Scan(&item.ID, &item.RootPath, &item.RelativePath) == nil {
			exports = append(exports, item)
		}
	}
	rows.Close()
	for _, item := range exports {
		root := filepath.Clean(item.RootPath)
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(item.RelativePath)))
		if path == root || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if result, err := a.db.ExecContext(ctx, `DELETE FROM tgstrm.stream_exports WHERE id=$1`, item.ID); err == nil {
			if deleted, _ := result.RowsAffected(); deleted > 0 {
				removeEmptyExportParents(path, root)
			}
		}
	}
}

func (a *API) replaceEpisodeExport(ctx context.Context, exportID, oldAssetID, oldMessageID string, item exportCandidate, rootPath, relative string, content, hash []byte, topicTitle string) bool {
	result, err := a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET asset_id=$2,content_hash=$3,status='pending',last_error=NULL,updated_at=now()
		WHERE id=$1 AND asset_id=$4`, exportID, item.AssetID, hash, oldAssetID)
	if err != nil {
		return false
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return false
	}
	path := filepath.Join(rootPath, filepath.FromSlash(relative))
	if err = atomicWrite(path, content); err != nil {
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET asset_id=$2,status='written',last_error=NULL,updated_at=now() WHERE id=$1`, exportID, oldAssetID)
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.stream_assets SET processed_at=now(),last_export_error=$2,updated_at=now() WHERE id=$1`, item.AssetID, fmt.Sprintf("No se pudo sustituir el STRM: %.400s", err.Error()))
		return false
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET status='written',written_at=now(),last_error=NULL,updated_at=now() WHERE id=$1`, exportID); err != nil {
		return false
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,processed_at=now(),last_export_error=NULL,updated_at=now() WHERE id=$1`, item.AssetID); err != nil {
		return false
	}
	var replacementID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.episode_replacements(topic_id,old_message_id,new_message_id)
		SELECT m.topic_id,$1::bigint,$2::bigint FROM tgstrm.messages m WHERE m.id=$2::bigint
		ON CONFLICT(old_message_id,new_message_id) DO UPDATE SET status='pending',last_error=NULL,updated_at=now()
		RETURNING id::text`, oldMessageID, item.MessageID).Scan(&replacementID); err != nil {
		return false
	}
	if err = tx.Commit(); err != nil {
		return false
	}
	if a.telegram != nil {
		deleteCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		deleteErr := a.telegram.DeleteVideoMessage(deleteCtx, oldMessageID)
		cancel()
		if deleteErr == nil {
			a.completeEpisodeReplacement(ctx, replacementID, topicTitle)
		} else {
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.episode_replacements SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, replacementID, fmt.Sprintf("%.500s", deleteErr.Error()))
		}
	}
	return true
}

func (a *API) completeEpisodeReplacement(ctx context.Context, replacementID, topicTitle string) {
	result, err := a.db.ExecContext(ctx, `UPDATE tgstrm.episode_replacements SET status='completed',last_error=NULL,completed_at=now(),updated_at=now()
		WHERE id=$1 AND status<>'completed'`, replacementID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return
	}
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail,group_id,topic_id,item_count)
		SELECT 'episode_replaced','completed',$2,'Episodio sustituido · se conservó el archivo nuevo y se eliminó el anterior',t.chat_id,er.topic_id,1
		FROM tgstrm.episode_replacements er JOIN tgstrm.topics t ON t.id=er.topic_id WHERE er.id=$1`, replacementID, topicTitle)
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		SELECT 'activity.changed','group',t.chat_id,jsonb_build_object('group_id',t.chat_id::text,'reason','episode_replaced')
		FROM tgstrm.episode_replacements er JOIN tgstrm.topics t ON t.id=er.topic_id WHERE er.id=$1`, replacementID)
}

func (a *API) generateTopicSTRM(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var input strmExportInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, "invalid_json", "Revisa los datos enviados.")
		return
	}
	// Automatic reconciliation, import completion and a manual action may all
	// request the same export at once. Serialize by topic so path reservation
	// and atomic file replacement remain one coherent operation.
	lockKey := r.PathValue("groupID") + ":" + r.PathValue("topicID")
	lockValue, _ := a.exportLocks.LoadOrStore(lockKey, &sync.Mutex{})
	exportLock := lockValue.(*sync.Mutex)
	exportLock.Lock()
	defer exportLock.Unlock()

	var topicTitle, libraryKind string
	err := a.db.QueryRowContext(r.Context(), `SELECT t.title,t.library_kind FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.is_active`, r.PathValue("topicID"), r.PathValue("groupID")).Scan(&topicTitle, &libraryKind)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "topic_not_found", "El topic no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "topic_failed", "No se pudo preparar el topic.")
		return
	}
	mediaType := map[string]string{"movie_collection": "movies", "series": "shows"}[libraryKind]
	if mediaType == "" {
		fail(w, 422, "topic_not_classified", "Clasifica el topic antes de generar su biblioteca.")
		return
	}

	settings := a.mediaSettingsDefaults()
	var raw []byte
	if err := a.db.QueryRowContext(r.Context(), `SELECT value FROM tgstrm.settings WHERE key='media'`).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &settings)
	} else if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "settings_failed", "No se pudo leer la configuración.")
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.PublicBaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(input.PublicBaseURL), "/")
	}
	if baseURL == "" {
		fail(w, 422, "public_url_required", "Configura la URL pública antes de generar STRM.")
		return
	}
	if settings.PublicBaseURL == "" {
		settings.PublicBaseURL = baseURL
		settingsRaw, marshalErr := json.Marshal(settings)
		if marshalErr != nil {
			fail(w, 500, "settings_failed", "No se pudo preparar la URL pública.")
			return
		}
		if _, saveErr := a.db.ExecContext(r.Context(), `INSERT INTO tgstrm.settings(key,value) VALUES('media',$1)
		 ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,revision=tgstrm.settings.revision+1,updated_at=now()`, settingsRaw); saveErr != nil {
			fail(w, 500, "settings_failed", "No se pudo fijar la URL pública.")
			return
		}
	}
	rootPath := settings.MoviesPath
	if mediaType == "shows" {
		rootPath = settings.ShowsPath
	}
	if rootPath == "" {
		rootPath = filepath.Join(a.store.Dir(), "library", mediaType)
	}
	if settings.SeriesNaming == "" {
		settings.SeriesNaming = "universal"
	}

	selected := make(map[string]bool, len(input.MessageIDs))
	for _, id := range input.MessageIDs {
		selected[id] = true
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT m.id::text,sa.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,''),m.telegram_message_id,
	 COALESCE(eo.season_number,0),COALESCE(eo.episode_number,0),COALESCE(eo.episode_title,''),
	 COALESCE(tm.title,''),COALESCE(to_char(tm.release_date,'YYYY'),''),COALESCE(tm.tmdb_id,0),m.sent_at
	 FROM tgstrm.messages m JOIN tgstrm.stream_assets sa ON sa.message_id=m.id JOIN tgstrm.message_media mm ON mm.message_id=m.id
	 LEFT JOIN tgstrm.stream_episode_overrides eo ON eo.asset_id=sa.id
	 LEFT JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id AND tm.media_type='movie'
	 WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`, r.PathValue("topicID"))
	if err != nil {
		fail(w, 500, "videos_failed", "No se pudieron preparar los vídeos.")
		return
	}
	defer rows.Close()
	var candidates []exportCandidate
	for rows.Next() {
		var item exportCandidate
		if err = rows.Scan(&item.MessageID, &item.AssetID, &item.FileName, &item.Caption, &item.TelegramID,
			&item.OverrideSeason, &item.OverrideEpisode, &item.OverrideTitle,
			&item.MovieTitle, &item.MovieYear, &item.MovieTMDBID, &item.SentAt); err != nil {
			fail(w, 500, "videos_failed", "No se pudieron preparar los vídeos.")
			return
		}
		if len(selected) == 0 || selected[item.MessageID] {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		fail(w, 422, "no_videos", "No hay vídeos seleccionados para generar.")
		return
	}

	var libraryID string
	err = a.db.QueryRowContext(r.Context(), `INSERT INTO tgstrm.library_targets(name,media_type,root_path,public_base_url) VALUES($1,$2,$3,$4)
	 ON CONFLICT(root_path,media_type) DO UPDATE SET public_base_url=EXCLUDED.public_base_url,updated_at=now() RETURNING id::text`, map[string]string{"movies": "Películas", "shows": "Series"}[mediaType], mediaType, rootPath, baseURL).Scan(&libraryID)
	if err != nil {
		fail(w, 500, "library_failed", "No se pudo preparar la biblioteca.")
		return
	}

	seriesTitle, seriesYear := splitTMDBTitleYear(topicTitle)
	var seriesTMDBID int64
	if mediaType == "shows" {
		var matchedTitle, matchedYear sql.NullString
		var matchedID sql.NullInt64
		matchErr := a.db.QueryRowContext(r.Context(), `SELECT tm.title,to_char(tm.release_date,'YYYY'),tm.tmdb_id
		 FROM tgstrm.tmdb_matches tm WHERE tm.topic_id::text=$1 AND tm.message_id IS NULL AND tm.media_type='tv'
		 ORDER BY tm.confirmed_at DESC LIMIT 1`, r.PathValue("topicID")).Scan(&matchedTitle, &matchedYear, &matchedID)
		if matchErr == nil {
			if matchedTitle.Valid && strings.TrimSpace(matchedTitle.String) != "" {
				seriesTitle = matchedTitle.String
			}
			if matchedYear.Valid {
				seriesYear = matchedYear.String
			}
			if matchedID.Valid {
				seriesTMDBID = matchedID.Int64
			}
		}
	}
	written, unresolved := 0, 0
	var replacementStartedAt time.Time
	var replacementRaw []byte
	if err := a.db.QueryRowContext(r.Context(), `SELECT value FROM tgstrm.settings WHERE key='episode_replacement_started_at'`).Scan(&replacementRaw); err == nil {
		var value string
		if json.Unmarshal(replacementRaw, &value) == nil {
			replacementStartedAt, _ = time.Parse(time.RFC3339Nano, value)
		}
	}
	for _, item := range candidates {
		var relative string
		if mediaType == "shows" {
			episode, ok := episodeIdentity{}, false
			if item.OverrideEpisode > 0 {
				episode, ok = episodeIdentity{Season: item.OverrideSeason, Episode: item.OverrideEpisode, Source: "manual"}, true
			} else {
				episode, ok = detectEpisode(item.FileName, item.Caption)
			}
			if !ok {
				unresolved++
				_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,
					processed_at=now(),last_export_error='No se pudo identificar temporada y episodio.',updated_at=now() WHERE id=$1`, item.AssetID)
				continue
			}
			episodeTitle := item.OverrideTitle
			if episodeTitle == "" {
				episodeTitle = a.tmdbEpisodeTitle(r.Context(), seriesTMDBID, episode.Season, episode.Episode)
			}
			relative = seriesSTRMPath(settings.SeriesNaming, seriesTitle, seriesYear, seriesTMDBID, episode, episodeTitle)
		} else {
			var ok bool
			relative, ok = movieSTRMPath(item.MovieTitle, item.MovieYear)
			if !ok || item.MovieTMDBID <= 0 {
				unresolved++
				a.removeAssetExportsForMediaType(r.Context(), item.AssetID, "movies")
				_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,
					processed_at=now(),last_export_error='Confirma la película y el año en TMDB antes de generar el STRM.',updated_at=now() WHERE id=$1`, item.AssetID)
				continue
			}
		}
		content := []byte(baseURL + "/stream/" + item.AssetID + "\n")
		hash := sha256.Sum256(content)
		if mediaType == "shows" {
			var existingExportID, existingAssetID, existingMessageID string
			var existingTelegramID int
			err = a.db.QueryRowContext(r.Context(), `SELECT e.id::text,e.asset_id::text,m.id::text,m.telegram_message_id
				FROM tgstrm.stream_exports e JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
				JOIN tgstrm.messages m ON m.id=sa.message_id
				WHERE e.library_id=$1 AND e.relative_path=$2 AND e.asset_id<>$3`, libraryID, filepath.ToSlash(relative), item.AssetID).
				Scan(&existingExportID, &existingAssetID, &existingMessageID, &existingTelegramID)
			if err == nil {
				if !shouldReplaceEpisode(replacementStartedAt, item.SentAt, item.TelegramID, existingTelegramID) {
					unresolved++
					_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,processed_at=now(),
						last_export_error='Este episodio ya tiene un STRM generado. La sustitución automática solo se aplica a archivos nuevos.',updated_at=now() WHERE id=$1`, item.AssetID)
					continue
				}
				if a.replaceEpisodeExport(r.Context(), existingExportID, existingAssetID, existingMessageID, item, rootPath, relative, content, hash[:], topicTitle) {
					written++
					continue
				}
				unresolved++
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				unresolved++
				_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_at=now(),last_export_error=$2,updated_at=now() WHERE id=$1`,
					item.AssetID, fmt.Sprintf("No se pudo comprobar si el episodio ya existía: %.400s", err.Error()))
				continue
			}
		}
		if mediaType == "movies" {
			var occupied bool
			if checkErr := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(
				SELECT 1 FROM tgstrm.stream_exports WHERE library_id=$1 AND relative_path=$2 AND asset_id<>$3
			)`, libraryID, filepath.ToSlash(relative), item.AssetID).Scan(&occupied); checkErr != nil {
				_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_at=now(),last_export_error=$2,updated_at=now() WHERE id=$1`,
					item.AssetID, fmt.Sprintf("No se pudo comprobar la ruta de la película: %.400s", checkErr.Error()))
				continue
			}
			if occupied {
				a.removeAssetExportsForMediaType(r.Context(), item.AssetID, "movies")
				_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,
					processed_at=now(),last_export_error='Esta película ya tiene un STRM canónico generado.',updated_at=now() WHERE id=$1`, item.AssetID)
				unresolved++
				continue
			}
		}
		var previousRelative sql.NullString
		_ = a.db.QueryRowContext(r.Context(), `SELECT relative_path FROM tgstrm.stream_exports WHERE library_id=$1 AND asset_id=$2`, libraryID, item.AssetID).Scan(&previousRelative)
		var exportID string
		err = a.db.QueryRowContext(r.Context(), `INSERT INTO tgstrm.stream_exports(library_id,asset_id,relative_path,content_hash,status) VALUES($1,$2,$3,$4,'pending')
		 ON CONFLICT(library_id,asset_id) DO UPDATE SET relative_path=EXCLUDED.relative_path,content_hash=EXCLUDED.content_hash,status='pending',last_error=NULL,updated_at=now() RETURNING id::text`, libraryID, item.AssetID, filepath.ToSlash(relative), hash[:]).Scan(&exportID)
		if err != nil {
			_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_at=now(),last_export_error=$2,updated_at=now() WHERE id=$1`,
				item.AssetID, fmt.Sprintf("No se pudo reservar la ruta STRM: %.400s", err.Error()))
			continue
		}
		if err = atomicWrite(filepath.Join(rootPath, relative), content); err != nil {
			_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_exports SET status='error',last_error=$2,updated_at=now() WHERE id=$1`, exportID, fmt.Sprintf("%.500s", err.Error()))
			_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_at=now(),last_export_error=$2,updated_at=now() WHERE id=$1`,
				item.AssetID, fmt.Sprintf("No se pudo escribir el STRM: %.400s", err.Error()))
			continue
		}
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_exports SET status='written',written_at=now(),last_error=NULL,updated_at=now() WHERE id=$1`, exportID)
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,
			processed_at=now(),last_export_error=NULL,updated_at=now() WHERE id=$1`, item.AssetID)
		if previousRelative.Valid && previousRelative.String != filepath.ToSlash(relative) {
			oldPath := filepath.Join(rootPath, filepath.FromSlash(previousRelative.String))
			if removeErr := os.Remove(oldPath); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				removeEmptyExportParents(oldPath, rootPath)
			}
		}
		a.removeStaleAssetExports(r.Context(), item.AssetID, mediaType)
		written++
	}
	if written > 0 || unresolved > 0 {
		_ = a.publishExportChange(r.Context(), r.PathValue("groupID"), r.PathValue("topicID"))
	}
	writeJSON(w, 200, map[string]any{"status": "completed", "written": written, "unresolved": unresolved, "requested": len(candidates), "root_path": rootPath, "media_type": mediaType})
}
