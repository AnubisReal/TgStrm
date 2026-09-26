package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

type topicArtworkItem struct {
	TopicID      string   `json:"topic_id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title,omitempty"`
	Year         string   `json:"year,omitempty"`
	BackdropPath string   `json:"backdrop_path,omitempty"`
	PosterPaths  []string `json:"poster_paths"`
}

func (a *API) startTopicArtworkScan(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	groupID := r.PathValue("groupID")
	var primary bool
	if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.settings WHERE key='active_chat_id' AND (value #>> '{}')=$1)`, groupID).Scan(&primary); err != nil || !primary {
		writeJSON(w, 202, map[string]string{"status": "skipped"})
		return
	}
	status := a.launchTopicArtworkScan(groupID)
	writeJSON(w, 202, map[string]string{"status": status})
}

func (a *API) launchTopicArtworkScan(groupID string) string {
	key := "artwork:" + groupID
	a.tmdbScanMu.Lock()
	if a.tmdbScans == nil {
		a.tmdbScans = make(map[string]bool)
	}
	if a.tmdbScans[key] {
		a.tmdbScanMu.Unlock()
		return "running"
	}
	a.tmdbScans[key] = true
	a.tmdbScanMu.Unlock()
	go func() {
		defer func() { a.tmdbScanMu.Lock(); delete(a.tmdbScans, key); a.tmdbScanMu.Unlock() }()
		a.runTopicArtworkScan(context.Background(), groupID)
	}()
	return "started"
}

// runTopicArtworkReconciler closes the gap left by topics created while this
// process was offline. Telegram restores the catalog; this worker enriches any
// newly recovered primary-library topic without requiring a browser visit.
func (a *API) runTopicArtworkReconciler(ctx context.Context) {
	refresh := func() {
		var groupID string
		if a.db.QueryRowContext(ctx, `SELECT c.id::text FROM tgstrm.settings s
			JOIN tgstrm.chats c ON c.id::text=(s.value #>> '{}') AND c.is_active
			WHERE s.key='active_chat_id'`).Scan(&groupID) == nil {
			a.launchTopicArtworkScan(groupID)
		}
	}
	refresh()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func (a *API) runTopicArtworkScan(ctx context.Context, groupID string) {
	for {
		var topicID int64
		var source string
		err := a.db.QueryRowContext(ctx, `SELECT t.id,t.title FROM tgstrm.topics t
		 LEFT JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL
		 LEFT JOIN tgstrm.tmdb_topic_scan_states ss ON ss.topic_id=t.id
		 WHERE t.chat_id::text=$1 AND t.is_active AND t.library_kind='series' AND tm.id IS NULL
		 AND (ss.topic_id IS NULL OR ss.source_text<>t.title OR (ss.status='failed' AND ss.checked_at < now()-interval '5 minutes'))
		 ORDER BY t.is_pinned DESC,t.top_message_id DESC LIMIT 1`, groupID).Scan(&topicID, &source)
		if err != nil {
			return
		}
		status := "not_found"
		if _, tmdbID, ok := splitTMDBIDMarker(source); ok {
			match, detailErr := a.localizedTVDetails(ctx, tmdbID)
			if detailErr != nil {
				status = "failed"
			} else if a.saveTopicTVMatch(ctx, topicID, match) == nil {
				status = "matched"
			}
			_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_topic_scan_states(topic_id,source_text,status) VALUES($1,$2,$3)
				ON CONFLICT(topic_id) DO UPDATE SET source_text=EXCLUDED.source_text,status=EXCLUDED.status,checked_at=now(),updated_at=now()`, topicID, source, status)
			if waitContext(ctx, 300*time.Millisecond) != nil {
				return
			}
			continue
		}
		title, year := splitTMDBTitleYear(source)
		var response struct {
			Results []struct {
				ID           int64   `json:"id"`
				Name         string  `json:"name"`
				OriginalName string  `json:"original_name"`
				FirstAirDate string  `json:"first_air_date"`
				PosterPath   string  `json:"poster_path"`
				BackdropPath string  `json:"backdrop_path"`
				Overview     string  `json:"overview"`
				Popularity   float64 `json:"popularity"`
			} `json:"results"`
		}
		values := url.Values{"query": {title}, "language": {"es-ES"}, "include_adult": {"false"}, "page": {"1"}}
		if year != "" {
			values.Set("first_air_date_year", year)
		}
		if err = a.tmdbRequestContext(ctx, "search/tv", values, &response); err != nil {
			status = "failed"
		} else {
			results := make([]tmdbResult, 0, len(response.Results))
			for _, item := range response.Results {
				results = append(results, tmdbResult{ID: item.ID, Title: item.Name, OriginalTitle: item.OriginalName, ReleaseDate: item.FirstAirDate, PosterPath: item.PosterPath, BackdropPath: item.BackdropPath, Overview: item.Overview, Popularity: item.Popularity})
			}
			if match, ok := tmdbExactMatch(source, results); ok {
				if err = a.saveTopicTVMatch(ctx, topicID, match); err == nil {
					status = "matched"
				}
			} else if len(results) > 0 {
				status = "ambiguous"
			}
		}
		_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_topic_scan_states(topic_id,source_text,status) VALUES($1,$2,$3)
		 ON CONFLICT(topic_id) DO UPDATE SET source_text=EXCLUDED.source_text,status=EXCLUDED.status,checked_at=now(),updated_at=now()`, topicID, source, status)
		if waitContext(ctx, 300*time.Millisecond) != nil {
			return
		}
	}
}

func (a *API) saveTopicTVMatch(ctx context.Context, topicID int64, match tmdbResult) error {
	metadata, _ := json.Marshal(match)
	_, err := a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date,poster_path,backdrop_path,overview,metadata)
		VALUES($1,NULL,'tv',$2,$3,$4,NULLIF($5,'')::date,NULLIF($6,''),NULLIF($7,''),$8,$9)
		ON CONFLICT(topic_id) WHERE message_id IS NULL DO UPDATE SET tmdb_id=EXCLUDED.tmdb_id,title=EXCLUDED.title,
		original_title=EXCLUDED.original_title,release_date=EXCLUDED.release_date,poster_path=EXCLUDED.poster_path,
		backdrop_path=EXCLUDED.backdrop_path,overview=EXCLUDED.overview,metadata=EXCLUDED.metadata,updated_at=now()`,
		topicID, match.ID, match.Title, match.OriginalTitle, match.ReleaseDate, match.PosterPath, match.BackdropPath, match.Overview, metadata)
	return err
}

func (a *API) topicArtwork(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	items := map[string]*topicArtworkItem{}
	rows, err := a.db.QueryContext(r.Context(), `SELECT tm.topic_id::text,tm.title,COALESCE(tm.release_date::text,''),COALESCE(tm.backdrop_path,''),COALESCE(tm.poster_path,'')
	 FROM tgstrm.tmdb_matches tm JOIN tgstrm.topics t ON t.id=tm.topic_id WHERE t.chat_id::text=$1 AND tm.message_id IS NULL`, groupID)
	if err != nil {
		fail(w, 500, "artwork_failed", "No se pudieron cargar las portadas.")
		return
	}
	for rows.Next() {
		var id, title, year, backdrop, poster string
		if rows.Scan(&id, &title, &year, &backdrop, &poster) == nil {
			paths := []string{}
			if poster != "" {
				paths = append(paths, poster)
			}
			items[id] = &topicArtworkItem{TopicID: id, Kind: "show", Title: title, Year: year, BackdropPath: backdrop, PosterPaths: paths}
		}
	}
	rows.Close()
	movieRows, err := a.db.QueryContext(r.Context(), `SELECT tm.topic_id::text,COALESCE(tm.poster_path,'') FROM tgstrm.tmdb_matches tm JOIN tgstrm.topics t ON t.id=tm.topic_id
	 WHERE t.chat_id::text=$1 AND tm.message_id IS NOT NULL AND tm.poster_path IS NOT NULL ORDER BY tm.topic_id,md5(tm.id::text)`, groupID)
	if err == nil {
		for movieRows.Next() {
			var id, poster string
			if movieRows.Scan(&id, &poster) != nil {
				continue
			}
			item := items[id]
			if item == nil {
				item = &topicArtworkItem{TopicID: id, Kind: "collection", PosterPaths: []string{}}
				items[id] = item
			}
			if item.Kind == "collection" && len(item.PosterPaths) < 3 {
				item.PosterPaths = append(item.PosterPaths, poster)
			}
		}
		movieRows.Close()
	}
	result := make([]*topicArtworkItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	key := "artwork:" + groupID
	a.tmdbScanMu.Lock()
	running := a.tmdbScans[key]
	a.tmdbScanMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"artwork": result, "running": running, "backdrop_base_url": "https://image.tmdb.org/t/p/w780", "poster_base_url": "https://image.tmdb.org/t/p/w342"})
}
