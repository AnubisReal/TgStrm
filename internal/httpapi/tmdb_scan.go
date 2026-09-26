package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var tmdbNoisePattern = regexp.MustCompile(`(?i)\b(480p|720p|1080p|2160p|4k|web[ ._-]?dl|webrip|bluray|brrip|hdrip|x26[45]|hevc|aac|eac3|ac3|dts|spanish|castellano)\b`)

func tmdbSource(body, fileName string) string {
	value := strings.TrimSpace(strings.Split(body, "\n")[0])
	if value == "" {
		value = strings.TrimSpace(fileName)
	}
	value = regexp.MustCompile(`\.[A-Za-z0-9]{2,5}$`).ReplaceAllString(value, "")
	value = tmdbNoisePattern.ReplaceAllString(value, " ")
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	return value
}

func normalizedTMDBTitle(value string) string {
	title, _ := splitTMDBTitleYear(value)
	var out strings.Builder
	space := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
			space = false
		} else if !space && out.Len() > 0 {
			out.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(out.String())
}

func tmdbExactMatch(source string, results []tmdbResult) (tmdbResult, bool) {
	_, requestedYear := splitTMDBTitleYear(source)
	wanted := normalizedTMDBTitle(source)
	for _, result := range results {
		if normalizedTMDBTitle(result.Title) != wanted && normalizedTMDBTitle(result.OriginalTitle) != wanted {
			continue
		}
		if requestedYear != "" && !strings.HasPrefix(result.ReleaseDate, requestedYear) {
			continue
		}
		return result, true
	}
	return tmdbResult{}, false
}

func (a *API) startTMDBScan(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	groupID, topicID := r.PathValue("groupID"), r.PathValue("topicID")
	var eligible bool
	if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.topics t JOIN tgstrm.settings s ON s.key='active_chat_id'
		WHERE t.id::text=$1 AND t.chat_id::text=$2 AND (s.value #>> '{}')=t.chat_id::text AND t.library_kind='movie_collection')`, topicID, groupID).Scan(&eligible); err != nil || !eligible {
		writeJSON(w, 202, map[string]any{"status": "skipped"})
		return
	}
	key := groupID + ":" + topicID
	a.tmdbScanMu.Lock()
	if a.tmdbScans == nil {
		a.tmdbScans = make(map[string]bool)
	}
	if a.tmdbScans[key] {
		a.tmdbScanMu.Unlock()
		writeJSON(w, 202, map[string]any{"status": "running"})
		return
	}
	a.tmdbScans[key] = true
	a.tmdbScanMu.Unlock()
	go func() {
		defer func() { a.tmdbScanMu.Lock(); delete(a.tmdbScans, key); a.tmdbScanMu.Unlock() }()
		a.runTMDBMovieScan(context.Background(), groupID, topicID)
	}()
	writeJSON(w, 202, map[string]any{"status": "started"})
}

// runMovieMatchReconciler makes movie enrichment a server responsibility. It
// resumes after restarts and does not depend on a user opening the management
// page. Only the configured primary group is processed automatically.
func (a *API) runMovieMatchReconciler(ctx context.Context) {
	a.processPendingPrimaryMovies(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.processPendingPrimaryMovies(ctx)
		}
	}
}

func (a *API) processPendingPrimaryMovies(ctx context.Context) {
	var groupID, topicID string
	err := a.db.QueryRowContext(ctx, `SELECT c.id::text,t.id::text
	 FROM tgstrm.settings active
	 JOIN tgstrm.chats c ON c.id::text=(active.value #>> '{}') AND c.is_active
	 JOIN tgstrm.topics t ON t.chat_id=c.id AND t.is_active AND t.library_kind='movie_collection'
	 WHERE active.key='active_chat_id' AND EXISTS(
	   SELECT 1 FROM tgstrm.messages m
	   LEFT JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id
	   LEFT JOIN tgstrm.tmdb_scan_states ss ON ss.message_id=m.id
	   WHERE m.topic_id=t.id AND m.is_active AND m.media_type='video' AND tm.id IS NULL
	   AND (ss.message_id IS NULL OR (ss.status='failed' AND ss.checked_at < now()-interval '5 minutes'))
	 ) ORDER BY t.id LIMIT 1`).Scan(&groupID, &topicID)
	if err != nil {
		return
	}
	key := groupID + ":" + topicID
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
	defer func() {
		a.tmdbScanMu.Lock()
		delete(a.tmdbScans, key)
		a.tmdbScanMu.Unlock()
	}()
	a.runTMDBMovieScan(ctx, groupID, topicID)
}

func (a *API) runTMDBMovieScan(ctx context.Context, groupID, topicID string) {
	for {
		var messageID int64
		var body, fileName string
		err := a.db.QueryRowContext(ctx, `SELECT m.id,COALESCE(m.body,''),COALESCE(mm.file_name,'')
		 FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
		 JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
		 LEFT JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id
		 LEFT JOIN tgstrm.tmdb_scan_states ss ON ss.message_id=m.id
		 WHERE c.id::text=$1 AND t.id::text=$2 AND m.is_active AND m.media_type='video' AND tm.id IS NULL
		 AND (ss.message_id IS NULL OR (ss.status='failed' AND ss.checked_at < now()-interval '5 minutes'))
		 ORDER BY m.telegram_message_id DESC LIMIT 1`, groupID, topicID).Scan(&messageID, &body, &fileName)
		if err != nil {
			return
		}
		source := tmdbSource(body, fileName)
		status := "not_found"
		if len([]rune(source)) >= 2 {
			var response struct {
				Results []struct {
					ID            int64   `json:"id"`
					Title         string  `json:"title"`
					OriginalTitle string  `json:"original_title"`
					ReleaseDate   string  `json:"release_date"`
					PosterPath    string  `json:"poster_path"`
					Overview      string  `json:"overview"`
					Popularity    float64 `json:"popularity"`
				} `json:"results"`
			}
			title, year := splitTMDBTitleYear(source)
			values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}, "page": {"1"}}
			if year != "" {
				values.Set("primary_release_year", year)
			}
			if err = a.tmdbRequestContext(ctx, "search/movie", values, &response); err != nil {
				status = "failed"
			} else {
				results := make([]tmdbResult, 0, len(response.Results))
				for _, item := range response.Results {
					results = append(results, tmdbResult{ID: item.ID, Title: item.Title, OriginalTitle: item.OriginalTitle, ReleaseDate: item.ReleaseDate, PosterPath: item.PosterPath, Overview: item.Overview, Popularity: item.Popularity})
				}
				if match, ok := tmdbExactMatch(source, results); ok {
					metadata, _ := json.Marshal(match)
					_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,overview,metadata)
					 SELECT m.topic_id,m.id,'movie',$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,''),$7,$8 FROM tgstrm.messages m WHERE m.id=$1
					 ON CONFLICT(message_id) WHERE message_id IS NOT NULL DO UPDATE SET tmdb_id=EXCLUDED.tmdb_id,title=EXCLUDED.title,original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,poster_path=EXCLUDED.poster_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,updated_at=now()`, messageID, match.ID, match.Title, match.OriginalTitle, match.ReleaseDate, match.PosterPath, match.Overview, metadata)
					if err == nil {
						status = "matched"
					}
				} else if len(results) > 0 {
					status = "ambiguous"
				}
			}
		}
		_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_scan_states(message_id,source_text,status) VALUES($1,$2,$3)
		 ON CONFLICT(message_id) DO UPDATE SET source_text=EXCLUDED.source_text,status=EXCLUDED.status,checked_at=now(),updated_at=now()`, messageID, source, status)
		// A modest pace keeps the background enrichment below TMDB's adaptive limits.
		if waitContext(ctx, 275*time.Millisecond) != nil {
			return
		}
	}
}

func (a *API) tmdbScanStatus(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("groupID") + ":" + r.PathValue("topicID")
	a.tmdbScanMu.Lock()
	running := a.tmdbScans[key]
	a.tmdbScanMu.Unlock()
	var total, matched, reviewed int
	_ = a.db.QueryRowContext(r.Context(), `SELECT count(*),count(tm.id),count(ss.message_id) FROM tgstrm.messages m
	 LEFT JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id LEFT JOIN tgstrm.tmdb_scan_states ss ON ss.message_id=m.id
	 WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video'`, r.PathValue("topicID")).Scan(&total, &matched, &reviewed)
	writeJSON(w, 200, map[string]any{"running": running, "total": total, "matched": matched, "reviewed": reviewed})
}
