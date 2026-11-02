package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type tmdbResult struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   string  `json:"release_date"`
	PosterPath    string  `json:"poster_path,omitempty"`
	BackdropPath  string  `json:"backdrop_path,omitempty"`
	Overview      string  `json:"overview"`
	Popularity    float64 `json:"popularity"`
}

type tmdbResponseError struct{ status int }

func (e tmdbResponseError) Error() string { return fmt.Sprintf("TMDB returned status %d", e.status) }

var tmdbAPIKeyPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
var tmdbParenthesizedYearPattern = regexp.MustCompile(`(?i)\s*[\[(]((?:19|20)\d{2})[\])]\s*$`)
var tmdbTrailingYearPattern = regexp.MustCompile(`\s+((?:19|20)\d{2})\s*$`)
var tmdbIDMarkerPattern = regexp.MustCompile(`(?i)\s*\[tmdbid-([1-9][0-9]*)\]\s*$`)

const tmdbRequestInterval = 100 * time.Millisecond // 10 req/s, below TMDB's documented approximate upper limit.
const tmdbMaxInlineWait = 12 * time.Second         // Do not leave a web request hanging for a long provider cooldown.

func (a *API) waitTMDBRequestSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.tmdbRateMu.Lock()
	now := time.Now()
	readyAt := a.tmdbNextRequest
	if a.tmdbBlockedUntil.After(readyAt) {
		readyAt = a.tmdbBlockedUntil
	}
	if readyAt.Before(now) {
		readyAt = now
	}
	a.tmdbNextRequest = readyAt.Add(tmdbRequestInterval)
	a.tmdbRateMu.Unlock()
	delay := time.Until(readyAt)
	if delay > tmdbMaxInlineWait {
		return tmdbResponseError{status: http.StatusTooManyRequests}
	}
	return waitContext(ctx, delay)
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *API) deferTMDBRequests(retryAfter string) {
	delay := time.Second
	if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil && seconds >= 0 {
		maxSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
		if seconds > maxSeconds {
			delay = time.Duration(1<<63 - 1)
		} else {
			delay = time.Duration(seconds) * time.Second
		}
	} else if retryAt, err := http.ParseTime(retryAfter); err == nil {
		delay = time.Until(retryAt)
	}
	if delay < time.Second {
		delay = time.Second
	}
	a.tmdbRateMu.Lock()
	blockedUntil := time.Now().Add(delay)
	if blockedUntil.After(a.tmdbBlockedUntil) {
		a.tmdbBlockedUntil = blockedUntil
	}
	a.tmdbRateMu.Unlock()
}

func splitTMDBIDMarker(value string) (string, int64, bool) {
	value = strings.TrimSpace(value)
	match := tmdbIDMarkerPattern.FindStringSubmatchIndex(value)
	if len(match) < 4 {
		return value, 0, false
	}
	id, err := strconv.ParseInt(value[match[2]:match[3]], 10, 64)
	if err != nil || id <= 0 {
		return value, 0, false
	}
	return strings.TrimSpace(value[:match[0]]), id, true
}

var tmdbYearRangePattern = regexp.MustCompile(`\s*[\(\[]((?:19|20)\d{2})\s*[-–—]\s*(?:19|20)\d{2}[\)\]]\s*$`)

func splitTMDBTitleYear(value string) (string, string) {
	value, _, _ = splitTMDBIDMarker(value)
	for _, pattern := range []*regexp.Regexp{tmdbYearRangePattern, tmdbParenthesizedYearPattern, tmdbTrailingYearPattern} {
		match := pattern.FindStringSubmatchIndex(value)
		if len(match) >= 4 {
			year := value[match[2]:match[3]]
			title := strings.TrimSpace(value[:match[0]])
			if title != "" {
				return title, year
			}
		}
	}
	return value, ""
}

func (a *API) tmdbToken(ctx context.Context) ([]byte, error) {
	var encrypted []byte
	if err := a.db.QueryRowContext(ctx, `SELECT ciphertext FROM tgstrm.integration_secrets WHERE key='tmdb_read_token'`).Scan(&encrypted); err != nil {
		return nil, err
	}
	return a.store.Unseal("integrations/tmdb/read-token", encrypted)
}

func normalizeTMDBType(value string) (string, bool) {
	switch value {
	case "movie", "tv":
		return value, true
	case "movies":
		return "movie", true
	case "shows":
		return "tv", true
	default:
		return "", false
	}
}

func (a *API) tmdbRequest(r *http.Request, path string, query url.Values, target any) error {
	return a.tmdbRequestContext(r.Context(), path, query, target)
}

func (a *API) tmdbRequestContext(ctx context.Context, path string, query url.Values, target any) error {
	if err := a.waitTMDBRequestSlot(ctx); err != nil {
		return err
	}
	token, err := a.tmdbToken(ctx)
	if err != nil {
		return err
	}
	endpoint := "https://api.themoviedb.org/3/" + strings.TrimLeft(path, "/")
	if query == nil {
		query = make(url.Values)
	}
	if tmdbAPIKeyPattern.Match(token) {
		query.Set("api_key", string(token))
	}
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if !tmdbAPIKeyPattern.Match(token) {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	req.Header.Set("Accept", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusTooManyRequests {
			a.deferTMDBRequests(response.Header.Get("Retry-After"))
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return tmdbResponseError{status: response.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target)
}

func (a *API) searchTMDB(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	mediaType, ok := normalizeTMDBType(r.URL.Query().Get("media_type"))
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	id, direct := manualTMDBSearchID(query)
	if !ok || (!direct && len([]rune(query)) < 2) || len([]rune(query)) > 160 {
		fail(w, 422, "invalid_tmdb_search", "Indica el tipo y un título o ID de TMDB válido.")
		return
	}
	if direct {
		var result tmdbResult
		var err error
		if mediaType == "tv" {
			result, err = a.localizedTVDetails(r.Context(), id)
		} else {
			result, err = a.localizedMovieDetails(r.Context(), id)
		}
		var responseErr tmdbResponseError
		if err == nil {
			writeJSON(w, 200, map[string]any{"media_type": mediaType, "query": query, "image_base_url": "https://image.tmdb.org/t/p/w342", "results": []tmdbResult{result}})
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 422, "tmdb_not_configured", "Configura y verifica TMDB antes de buscar.")
			return
		}
		if errors.As(err, &responseErr) && (responseErr.status == http.StatusUnauthorized || responseErr.status == http.StatusForbidden) {
			fail(w, 422, "tmdb_rejected", "TMDB rechazó la credencial configurada. Revísala en Ajustes.")
			return
		}
		if errors.As(err, &responseErr) && responseErr.status == http.StatusNotFound {
			// Numeric titles are still searchable when no such identifier exists.
			if strings.HasPrefix(strings.ToLower(query), "tmdb:") {
				writeJSON(w, 200, map[string]any{"media_type": mediaType, "query": query, "results": []tmdbResult{}})
				return
			}
		} else {
			fail(w, 502, "tmdb_search_failed", "No se pudo consultar ese ID en TMDB. Comprueba la configuración y vuelve a intentarlo.")
			return
		}
	}
	var response struct {
		Results []struct {
			ID            int64   `json:"id"`
			Title         string  `json:"title"`
			Name          string  `json:"name"`
			OriginalTitle string  `json:"original_title"`
			OriginalName  string  `json:"original_name"`
			ReleaseDate   string  `json:"release_date"`
			FirstAirDate  string  `json:"first_air_date"`
			PosterPath    string  `json:"poster_path"`
			BackdropPath  string  `json:"backdrop_path"`
			Overview      string  `json:"overview"`
			Popularity    float64 `json:"popularity"`
		} `json:"results"`
	}
	title, year := splitTMDBTitleYear(query)
	values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}, "page": {"1"}}
	if year != "" {
		if mediaType == "movie" {
			values.Set("primary_release_year", year)
		} else {
			values.Set("first_air_date_year", year)
		}
	}
	err := a.tmdbRequest(r, "search/"+mediaType, values, &response)
	var responseErr tmdbResponseError
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 422, "tmdb_not_configured", "Configura y verifica TMDB antes de buscar.")
		return
	} else if errors.As(err, &responseErr) && (responseErr.status == http.StatusUnauthorized || responseErr.status == http.StatusForbidden) {
		fail(w, 422, "tmdb_rejected", "TMDB rechazó el token configurado. Sustitúyelo y vuelve a verificarlo en Ajustes.")
		return
	} else if err != nil {
		fail(w, 502, "tmdb_search_failed", "TMDB no pudo completar la búsqueda.")
		return
	}
	results := make([]tmdbResult, 0, min(10, len(response.Results)))
	for _, item := range response.Results {
		result := tmdbResult{ID: item.ID, Title: item.Title, OriginalTitle: item.OriginalTitle, ReleaseDate: item.ReleaseDate, PosterPath: item.PosterPath, BackdropPath: item.BackdropPath, Overview: item.Overview, Popularity: item.Popularity}
		if mediaType == "tv" {
			result.Title, result.OriginalTitle, result.ReleaseDate = item.Name, item.OriginalName, item.FirstAirDate
		}
		results = append(results, result)
		if len(results) == 10 {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"media_type": mediaType, "query": query, "image_base_url": "https://image.tmdb.org/t/p/w342", "results": results})
}

func manualTMDBSearchID(query string) (int64, bool) {
	value := strings.TrimSpace(query)
	if strings.HasPrefix(strings.ToLower(value), "tmdb:") {
		value = strings.TrimSpace(value[5:])
	}
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(value, 10, 32)
	return id, err == nil && id > 0
}

type confirmTMDBInput struct {
	MediaType string `json:"media_type"`
	TMDBID    int64  `json:"tmdb_id"`
	MessageID string `json:"message_id,omitempty"`
}

func (a *API) confirmTMDBMatch(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var input confirmTMDBInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.TMDBID <= 0 {
		fail(w, 400, "invalid_tmdb_match", "Selecciona un resultado válido de TMDB.")
		return
	}
	mediaType, ok := normalizeTMDBType(input.MediaType)
	if !ok || (mediaType == "movie" && input.MessageID == "") || (mediaType == "tv" && input.MessageID != "") {
		fail(w, 422, "invalid_tmdb_scope", "Las películas se asocian a un archivo y las series al topic completo.")
		return
	}
	var topicID int64
	err := a.db.QueryRowContext(r.Context(), `SELECT t.id FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
	 WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.is_active`, r.PathValue("topicID"), r.PathValue("groupID")).Scan(&topicID)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "topic_not_found", "El topic no está disponible.")
		return
	} else if err != nil {
		fail(w, 500, "topic_failed", "No se pudo comprobar el topic.")
		return
	}
	var messageID any
	if input.MessageID != "" {
		var stored int64
		if err = a.db.QueryRowContext(r.Context(), `SELECT id FROM tgstrm.messages WHERE id::text=$1 AND topic_id=$2 AND is_active AND media_type='video'`, input.MessageID, topicID).Scan(&stored); err != nil {
			fail(w, 422, "video_not_found", "El vídeo seleccionado no pertenece a este topic.")
			return
		}
		messageID = stored
	}
	var details struct {
		ID            int64  `json:"id"`
		Title         string `json:"title"`
		Name          string `json:"name"`
		OriginalTitle string `json:"original_title"`
		OriginalName  string `json:"original_name"`
		ReleaseDate   string `json:"release_date"`
		FirstAirDate  string `json:"first_air_date"`
		PosterPath    string `json:"poster_path"`
		BackdropPath  string `json:"backdrop_path"`
		Overview      string `json:"overview"`
	}
	if err = a.tmdbRequest(r, mediaType+"/"+strconv.FormatInt(input.TMDBID, 10), url.Values{"language": {"es-ES"}}, &details); err != nil {
		fail(w, 502, "tmdb_details_failed", "TMDB no pudo confirmar ese título.")
		return
	}
	result := tmdbResult{ID: details.ID, Title: details.Title, OriginalTitle: details.OriginalTitle, ReleaseDate: details.ReleaseDate, PosterPath: details.PosterPath, BackdropPath: details.BackdropPath, Overview: details.Overview}
	if mediaType == "tv" {
		result.Title, result.OriginalTitle, result.ReleaseDate = details.Name, details.OriginalName, details.FirstAirDate
	}
	metadata, _ := json.Marshal(result)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "tmdb_match_failed", "No se pudo iniciar la asociación.")
		return
	}
	defer tx.Rollback()
	if messageID == nil {
		var locked bool
		if err = tx.QueryRowContext(r.Context(), `SELECT pg_try_advisory_xact_lock(hashtextextended('topic-identity:' || $1::text,0))`, strconv.FormatInt(topicID, 10)).Scan(&locked); err != nil || !locked {
			fail(w, 409, "correction_in_progress", "La corrección está aplicando un cambio. Vuelve a intentarlo en unos segundos.")
			return
		}
		var duplicate bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.tmdb_matches other JOIN tgstrm.topics t ON t.id=other.topic_id WHERE t.chat_id::text=$1 AND t.is_active AND t.id<>$2 AND other.message_id IS NULL AND other.media_type='tv' AND other.tmdb_id=$3 AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.destination_topic_id=$2))`, r.PathValue("groupID"), topicID, result.ID).Scan(&duplicate); err != nil {
			fail(w, 500, "tmdb_match_failed", "No se pudo comprobar la serie.")
			return
		}
		if duplicate {
			fail(w, 409, "series_already_present", "Esa serie ya está asociada a otro topic del cajón. No se sobrescribirá su biblioteca.")
			return
		}
	}
	var matchID string
	if messageID == nil {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,backdrop_path,overview,metadata)
		 VALUES($1,NULL,$2,$3,$4,$5,NULLIF($6,'')::date,NULLIF($7,''),NULLIF($8,''),$9,$10)
		 ON CONFLICT(topic_id) WHERE message_id IS NULL DO UPDATE SET media_type=EXCLUDED.media_type,tmdb_id=EXCLUDED.tmdb_id,title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,poster_path=EXCLUDED.poster_path,backdrop_path=EXCLUDED.backdrop_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,confirmed_at=now(),updated_at=now() RETURNING id::text`, topicID, mediaType, result.ID, result.Title, result.OriginalTitle, result.ReleaseDate, result.PosterPath, result.BackdropPath, result.Overview, metadata).Scan(&matchID)
	} else {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,overview,metadata)
		 VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::date,NULLIF($8,''),$9,$10)
		 ON CONFLICT(message_id) WHERE message_id IS NOT NULL DO UPDATE SET media_type=EXCLUDED.media_type,tmdb_id=EXCLUDED.tmdb_id,title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,poster_path=EXCLUDED.poster_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,confirmed_at=now(),updated_at=now() RETURNING id::text`, topicID, messageID, mediaType, result.ID, result.Title, result.OriginalTitle, result.ReleaseDate, result.PosterPath, result.Overview, metadata).Scan(&matchID)
	}
	if err != nil {
		fail(w, 500, "tmdb_match_failed", "No se pudo guardar la asociación de TMDB.")
		return
	}
	if messageID == nil {
		if _, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_identity_jobs SET revision=revision+1,status='pending',last_error=NULL,next_attempt_at=now(),updated_at=now() WHERE topic_id=$1 AND status='attention'`, topicID); err != nil {
			fail(w, 500, "tmdb_match_failed", "No se pudo preparar la corrección.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "tmdb_match_failed", "No se pudo guardar la asociación.")
		return
	}
	if messageID == nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM tgstrm.tmdb_topic_scan_states WHERE topic_id=$1`, topicID)
		_, _ = a.db.ExecContext(r.Context(), `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
			VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','tmdb_match_confirmed'))`, r.PathValue("groupID"))
	} else {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.tmdb_scan_states SET status='matched',checked_at=now(),updated_at=now()
			WHERE message_id=$1`, messageID)
		if code := a.generateSelectedTopicSTRM(r.Context(), r.PathValue("groupID"), r.PathValue("topicID"), input.MessageID); code != http.StatusOK {
			fail(w, 500, "strm_generation_failed", "La asociación se guardó, pero no se pudo generar todavía el STRM. El servidor volverá a intentarlo automáticamente.")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"id": matchID, "media_type": mediaType, "message_id": input.MessageID, "match": result})
}

func (a *API) listTMDBMatches(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT tm.id::text,tm.media_type,tm.tmdb_id,tm.title,tm.original_title,
	 COALESCE(tm.release_date::text,''),COALESCE(tm.poster_path,''),tm.overview,COALESCE(tm.message_id::text,'')
	 FROM tgstrm.tmdb_matches tm JOIN tgstrm.topics t ON t.id=tm.topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
	 WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.is_active ORDER BY tm.confirmed_at DESC`, r.PathValue("topicID"), r.PathValue("groupID"))
	if err != nil {
		fail(w, 500, "tmdb_matches_failed", "No se pudieron leer las asociaciones de TMDB.")
		return
	}
	defer rows.Close()
	type savedMatch struct {
		ID, MediaType, Title, OriginalTitle, ReleaseDate, PosterPath, Overview, MessageID string
		TMDBID                                                                            int64
	}
	results := make([]map[string]any, 0)
	for rows.Next() {
		var item savedMatch
		if err = rows.Scan(&item.ID, &item.MediaType, &item.TMDBID, &item.Title, &item.OriginalTitle, &item.ReleaseDate, &item.PosterPath, &item.Overview, &item.MessageID); err != nil {
			fail(w, 500, "tmdb_matches_failed", "No se pudieron leer las asociaciones de TMDB.")
			return
		}
		results = append(results, map[string]any{"id": item.ID, "media_type": item.MediaType, "tmdb_id": item.TMDBID, "title": item.Title, "original_title": item.OriginalTitle, "release_date": item.ReleaseDate, "poster_path": item.PosterPath, "overview": item.Overview, "message_id": item.MessageID})
	}
	writeJSON(w, 200, map[string]any{"matches": results, "image_base_url": "https://image.tmdb.org/t/p/w342"})
}
