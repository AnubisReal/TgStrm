package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type missingEpisode struct {
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title"`
	AirDate   string `json:"air_date,omitempty"`
	StillPath string `json:"still_path,omitempty"`
}

// topicEpisodeGaps compares TMDB's aired episode catalogue with episode videos
// present in Telegram. STRM generation is a separate library-export concern:
// a video must stop appearing as missing as soon as its episode identity is
// known, even when its STRM has not been written yet. The expected catalogue
// is cached in PostgreSQL; realtime refreshes therefore need no TMDB request.
func (a *API) topicEpisodeGaps(w http.ResponseWriter, r *http.Request) {
	var seriesID int64
	err := a.db.QueryRowContext(r.Context(), `SELECT tm.tmdb_id FROM tgstrm.topics t
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.id::text=$1 AND c.is_active
		JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL AND tm.media_type='tv'
		WHERE t.id::text=$2 AND t.is_active AND t.library_kind='series'`, r.PathValue("groupID"), r.PathValue("topicID")).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"missing": []missingEpisode{}})
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "episode_gaps_failed", "No se pudo comprobar el catálogo de episodios.")
		return
	}

	var cached int
	_ = a.db.QueryRowContext(r.Context(), `SELECT count(*) FROM tgstrm.tmdb_episode_metadata
		WHERE series_tmdb_id=$1 AND language='es-ES'`, seriesID).Scan(&cached)
	if cached == 0 || r.URL.Query().Get("refresh") == "1" {
		if err = a.refreshSeriesEpisodeMetadata(r, seriesID); err != nil && cached == 0 {
			fail(w, http.StatusBadGateway, "tmdb_episodes_failed", "TMDB no pudo entregar el catálogo de episodios.")
			return
		}
	}

	present := make(map[[2]int]struct{})
	rows, err := a.db.QueryContext(r.Context(), `SELECT COALESCE(mm.file_name,''),COALESCE(m.body,''),
		COALESCE(eo.season_number,0),COALESCE(eo.episode_number,0)
		FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
		LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
		LEFT JOIN tgstrm.stream_episode_overrides eo ON eo.asset_id=sa.id AND eo.series_tmdb_id=$2
		WHERE m.topic_id=$1 AND m.is_active AND m.media_type='video'`, r.PathValue("topicID"), seriesID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "episode_gaps_failed", "No se pudo comprobar la biblioteca.")
		return
	}
	for rows.Next() {
		var fileName, caption string
		var season, episode int
		if rows.Scan(&fileName, &caption, &season, &episode) != nil {
			continue
		}
		if episode == 0 {
			if detected, ok := detectEpisode(fileName, caption); ok {
				season, episode = detected.Season, detected.Episode
			}
		}
		if episode > 0 {
			present[[2]int{season, episode}] = struct{}{}
		}
	}
	rows.Close()

	rows, err = a.db.QueryContext(r.Context(), `SELECT season_number,episode_number,title,
		COALESCE(metadata->>'air_date',''),COALESCE(metadata->>'still_path','')
		FROM tgstrm.tmdb_episode_metadata WHERE series_tmdb_id=$1 AND language='es-ES'
		AND season_number>0 AND NULLIF(metadata->>'air_date','')::date<=CURRENT_DATE
		ORDER BY season_number,episode_number`, seriesID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "episode_gaps_failed", "No se pudo leer el catálogo de episodios.")
		return
	}
	defer rows.Close()
	missing := make([]missingEpisode, 0)
	for rows.Next() {
		var item missingEpisode
		if rows.Scan(&item.Season, &item.Episode, &item.Title, &item.AirDate, &item.StillPath) == nil {
			if _, exists := present[[2]int{item.Season, item.Episode}]; !exists {
				missing = append(missing, item)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"missing": missing, "image_base_url": "https://image.tmdb.org/t/p/w300"})
}

func (a *API) refreshSeriesEpisodeMetadata(r *http.Request, seriesID int64) error {
	var series struct {
		Seasons []struct {
			Number int `json:"season_number"`
			Count  int `json:"episode_count"`
		} `json:"seasons"`
	}
	if err := a.tmdbRequest(r, fmt.Sprintf("tv/%d", seriesID), url.Values{"language": {"es-ES"}}, &series); err != nil {
		return err
	}
	sort.Slice(series.Seasons, func(i, j int) bool { return series.Seasons[i].Number < series.Seasons[j].Number })
	for _, season := range series.Seasons {
		if season.Number <= 0 || season.Count == 0 {
			continue
		}
		var count int
		var newest sql.NullTime
		_ = a.db.QueryRowContext(r.Context(), `SELECT count(*),max(fetched_at) FROM tgstrm.tmdb_episode_metadata
			WHERE series_tmdb_id=$1 AND season_number=$2 AND language='es-ES'`, seriesID, season.Number).Scan(&count, &newest)
		if count >= season.Count && newest.Valid && time.Since(newest.Time) < 24*time.Hour {
			continue
		}
		var result struct {
			Episodes []struct {
				Number    int    `json:"episode_number"`
				Name      string `json:"name"`
				AirDate   string `json:"air_date"`
				StillPath string `json:"still_path"`
				Overview  string `json:"overview"`
			} `json:"episodes"`
		}
		if err := a.tmdbRequest(r, fmt.Sprintf("tv/%d/season/%d", seriesID, season.Number), url.Values{"language": {"es-ES"}}, &result); err != nil {
			return err
		}
		for _, episode := range result.Episodes {
			metadata, _ := json.Marshal(episode)
			_, err := a.db.ExecContext(r.Context(), `INSERT INTO tgstrm.tmdb_episode_metadata
				(series_tmdb_id,season_number,episode_number,language,title,metadata)
				VALUES($1,$2,$3,'es-ES',$4,$5) ON CONFLICT(series_tmdb_id,season_number,episode_number,language)
				DO UPDATE SET title=EXCLUDED.title,metadata=EXCLUDED.metadata,fetched_at=now(),updated_at=now()`,
				seriesID, season.Number, episode.Number, episode.Name, metadata)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
