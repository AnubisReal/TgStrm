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
	Season, Episode, EndEpisode int
	Source                      string
}

var episodePatterns = []struct {
	re      *regexp.Regexp
	season  int
	episode int
}{
	{regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])s(?:eason)?[ ._-]*(\d{1,2})[ ._-]*e(?:p(?:isode)?)?[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])t(?:emporada)?[ .,_-]*(\d{1,2})[ .,_-]*(?:e|ep|c|cap(?:itulo|ítulo)?)[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)(?:^|[^[:digit:]])(\d{1,2})x(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)season[ ._-]*(\d{1,2})[ ._-]*episode[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
	{regexp.MustCompile(`(?i)temporada[ ._-]*(\d{1,2}).{0,16}cap(?:itulo|ítulo)?[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`), 1, 2},
}

var episodeRangePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])s(\d{1,2})e(\d{1,3})[ ]*-[ ]*(?:s(\d{1,2}))?e?(\d{1,3})(?:[^[:alnum:]]|$)`)
var crossEpisodeRangePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(\d{1,2})x(\d{1,3})[ ]*-[ ]*(?:(\d{1,2})x)?(\d{1,3})(?:[^[:alnum:]]|$)`)

func (episode episodeIdentity) lastEpisode() int {
	if episode.EndEpisode > episode.Episode {
		return episode.EndEpisode
	}
	return episode.Episode
}
func (episode episodeIdentity) code() string {
	code := fmt.Sprintf("S%02dE%02d", episode.Season, episode.Episode)
	if episode.EndEpisode > episode.Episode {
		code += fmt.Sprintf("-E%02d", episode.EndEpisode)
	}
	return code
}

var compactChapterPattern = regexp.MustCompile(`(?i)(?:\[\s*)?cap(?:itulo|ítulo)?\.?[ ._-]*(\d{3,4})(?:\s*\])?`)

func detectEpisode(fileName, caption string) (episodeIdentity, bool) {
	for _, source := range []struct{ name, value string }{{"caption", caption}, {"filename", fileName}} {
		for _, pattern := range []*regexp.Regexp{episodeRangePattern, crossEpisodeRangePattern} {
			if match := pattern.FindStringSubmatch(source.value); len(match) == 5 {
				season, _ := strconv.Atoi(match[1])
				first, _ := strconv.Atoi(match[2])
				last, _ := strconv.Atoi(match[4])
				otherSeason := season
				if match[3] != "" {
					otherSeason, _ = strconv.Atoi(match[3])
				}
				// Invalid/cross-season ranges require review, never silently export the first episode.
				if season != otherSeason || first < 1 || last <= first || last-first > 100 {
					return episodeIdentity{}, false
				}
				return episodeIdentity{Season: season, Episode: first, EndEpisode: last, Source: source.name}, true
			}
		}
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
	// "N de M" needs matched-season context; don't mistake its prefix for a plain episode.
	if episodeFractionPattern.MatchString(fileName) || episodeFractionPattern.MatchString(caption) {
		return episodeIdentity{}, false
	}
	// Fall back only after checking both sources for complete identities.
	number, source, ok := seasonlessEpisodeNumber(fileName, caption)
	if !ok {
		number, source, ok = leadingEpisodeNumber(fileName, caption)
	}
	if ok {
		season := 1
		for _, text := range []string{caption, fileName} {
			if match := explicitSeasonPattern.FindStringSubmatch(text); len(match) == 2 {
				season, _ = strconv.Atoi(match[1])
				break
			}
		}
		return episodeIdentity{Season: season, Episode: number, Source: source}, true
	}
	return episodeIdentity{}, false
}

func seriesSTRMPath(profile, title, year string, tmdbID int64, episode episodeIdentity, episodeTitle string, includeTMDBID bool) string {
	label := strings.TrimSpace(title)
	if year != "" && !strings.Contains(label, "("+year+")") {
		label += " (" + year + ")"
	}
	label = safeLibraryName(label, "Serie")
	seasonFolder := fmt.Sprintf("Season %02d", episode.Season)
	code := episode.code()
	episodeTitle = safeLibraryName(episodeTitle, fmt.Sprintf("Episode %02d", episode.Episode))
	var fileName string
	switch profile {
	case "jellyfin_tmdb":
		folder := label
		if includeTMDBID && tmdbID > 0 {
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
