package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var cumulativeFilePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(\d{1,2})[xs](\d{1,3})[ ._-]+(.+)$`)

func episodeTitleKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type seasonTitleEntry struct {
	Number int    `json:"episode_number"`
	Name   string `json:"name"`
}

func uniqueSeasonTitle(title string, episodes []seasonTitleEntry) (int, bool) {
	key := episodeTitleKey(title)
	found, count := 0, 0
	for _, ep := range episodes {
		if key != "" && episodeTitleKey(ep.Name) == key {
			found = ep.Number
			count++
		}
	}
	return found, count == 1
}

// Only suspect an absolute number when it exceeds the matched season's range.
// Never subtract counts or use the file's position: specials and gaps shift both.
func (a *API) detectCumulativeEpisode(ctx context.Context, seriesID int64, file string) (episodeIdentity, bool, bool) {
	match := cumulativeFilePattern.FindStringSubmatch(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	if len(match) != 4 || seriesID <= 0 || a.db == nil {
		return episodeIdentity{}, false, false
	}
	season, _ := strconv.Atoi(match[1])
	number, _ := strconv.Atoi(match[2])
	var episodes []seasonTitleEntry
	rows, err := a.db.QueryContext(ctx, `SELECT episode_number,title FROM tgstrm.tmdb_episode_metadata WHERE series_tmdb_id=$1 AND season_number=$2 AND language='es-ES' AND metadata->>'complete_season'='true' ORDER BY episode_number`, seriesID, season)
	if err != nil {
		return episodeIdentity{Source: "context_unavailable"}, true, false
	}
	for rows.Next() {
		var ep seasonTitleEntry
		if err = rows.Scan(&ep.Number, &ep.Name); err != nil {
			rows.Close()
			return episodeIdentity{Source: "context_unavailable"}, true, false
		}
		episodes = append(episodes, ep)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return episodeIdentity{Source: "context_unavailable"}, true, false
	}
	if len(episodes) == 0 {
		var response struct {
			Episodes []seasonTitleEntry `json:"episodes"`
		}
		if a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d/season/%d", seriesID, season), url.Values{"language": {"es-ES"}}, &response) != nil {
			return episodeIdentity{Source: "context_unavailable"}, true, false
		}
		episodes = response.Episodes
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return episodeIdentity{Source: "context_unavailable"}, true, false
		}
		defer tx.Rollback()
		for _, ep := range episodes {
			metadata, _ := json.Marshal(map[string]any{"complete_season": true})
			if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_episode_metadata(series_tmdb_id,season_number,episode_number,language,title,metadata) VALUES($1,$2,$3,'es-ES',$4,$5) ON CONFLICT(series_tmdb_id,season_number,episode_number,language) DO UPDATE SET title=EXCLUDED.title,metadata=EXCLUDED.metadata,fetched_at=now(),updated_at=now()`, seriesID, season, ep.Number, ep.Name, metadata); err != nil {
				return episodeIdentity{Source: "context_unavailable"}, true, false
			}
		}
		if tx.Commit() != nil {
			return episodeIdentity{Source: "context_unavailable"}, true, false
		}
	}
	max := 0
	for _, ep := range episodes {
		if ep.Number > max {
			max = ep.Number
		}
	}
	if number <= max {
		return episodeIdentity{}, false, false
	}
	n, ok := uniqueSeasonTitle(match[3], episodes)
	return episodeIdentity{Season: season, Episode: n, Source: "filename"}, true, ok
}
