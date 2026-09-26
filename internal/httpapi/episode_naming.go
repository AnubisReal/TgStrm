package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type episodeIdentity struct {
	Season, Episode int
	Source          string
}

var episodePatterns = []struct {
	re      *regexp.Regexp
	season  int
	episode int
}{
	{regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])s(?:eason)?[ ._-]*(\d{1,2})[ ._-]*e(?:p(?:isode)?)?[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])t(?:emporada)?[ .,_-]*(\d{1,2})[ .,_-]*(?:e|ep|cap(?:itulo|ítulo)?)[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)(?:^|[^[:digit:]])(\d{1,2})x(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)season[ ._-]*(\d{1,2})[ ._-]*episode[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)temporada[ ._-]*(\d{1,2}).{0,16}cap(?:itulo|ítulo)?[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
}

var compactChapterPattern = regexp.MustCompile(`(?i)(?:\[\s*)?cap(?:itulo|ítulo)?\.?[ ._-]*(\d{3,4})(?:\s*\])?`)

func detectEpisode(fileName, caption string) (episodeIdentity, bool) {
	for _, source := range []struct{ name, value string }{{"filename", fileName}, {"caption", caption}} {
		for _, pattern := range episodePatterns {
			match := pattern.re.FindStringSubmatch(source.value)
			if len(match) == 3 {
				season, seasonErr := strconv.Atoi(match[pattern.season])
				episode, episodeErr := strconv.Atoi(match[pattern.episode])
				if seasonErr == nil && episodeErr == nil && season >= 0 && episode > 0 {
					return episodeIdentity{Season: season, Episode: episode, Source: source.name}, true
				}
			}
		}
		if match := compactChapterPattern.FindStringSubmatch(source.value); len(match) == 2 {
			chapter, err := strconv.Atoi(match[1])
			if err == nil && chapter >= 100 {
				season, episode := chapter/100, chapter%100
				if episode > 0 {
					return episodeIdentity{Season: season, Episode: episode, Source: source.name}, true
				}
			}
		}
	}
	return episodeIdentity{}, false
}

func seriesSTRMPath(profile, title, year string, tmdbID int64, episode episodeIdentity, episodeTitle string) string {
	label := strings.TrimSpace(title)
	if year != "" && !strings.Contains(label, "("+year+")") {
		label += " (" + year + ")"
	}
	label = safeLibraryName(label, "Serie")
	seasonFolder := fmt.Sprintf("Season %02d", episode.Season)
	code := fmt.Sprintf("S%02dE%02d", episode.Season, episode.Episode)
	episodeTitle = safeLibraryName(episodeTitle, fmt.Sprintf("Episode %02d", episode.Episode))
	var fileName string
	switch profile {
	case "jellyfin_tmdb":
		folder := label
		if tmdbID > 0 {
			folder += fmt.Sprintf(" [tmdbid-%d]", tmdbID)
		}
		fileName = fmt.Sprintf("%s %s - %s.strm", safeLibraryName(title, "Serie"), code, episodeTitle)
		return filepath.Join(folder, seasonFolder, fileName)
	case "compact":
		fileName = fmt.Sprintf("%s - %s.strm", code, episodeTitle)
	default:
		fileName = fmt.Sprintf("%s - %s - %s.strm", label, code, episodeTitle)
	}
	return filepath.Join(label, seasonFolder, fileName)
}

func (a *API) tmdbEpisodeTitle(ctx context.Context, seriesID int64, season, episode int) string {
	if seriesID <= 0 {
		return ""
	}
	var title string
	err := a.db.QueryRowContext(ctx, `SELECT title FROM tgstrm.tmdb_episode_metadata
	 WHERE series_tmdb_id=$1 AND season_number=$2 AND episode_number=$3 AND language='es-ES'`, seriesID, season, episode).Scan(&title)
	if err == nil {
		return title
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	var response struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("tv/%d/season/%d/episode/%d", seriesID, season, episode)
	if err = a.tmdbRequestContext(ctx, path, url.Values{"language": {"es-ES"}}, &response); err != nil || strings.TrimSpace(response.Name) == "" {
		return ""
	}
	metadata, _ := json.Marshal(response)
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_episode_metadata(series_tmdb_id,season_number,episode_number,language,title,metadata)
	 VALUES($1,$2,$3,'es-ES',$4,$5) ON CONFLICT(series_tmdb_id,season_number,episode_number,language)
	 DO UPDATE SET title=EXCLUDED.title,metadata=EXCLUDED.metadata,fetched_at=now(),updated_at=now()`, seriesID, season, episode, response.Name, metadata)
	return response.Name
}
