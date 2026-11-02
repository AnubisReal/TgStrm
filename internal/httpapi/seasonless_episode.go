package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A bare episode number is useful only when TMDB proves the season unambiguous.
var explicitSeasonPattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(?:temporada|season|s|t)[ ._-]*(\d{1,2})(?:[^[:alnum:]]|$)`)

var seasonlessEpisodePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(?:episodio|episode|ep|capítulo|capitulo|cap)[ ._-]*(\d{1,3})(?:[^[:digit:]]|$)`)

func seasonlessEpisodeNumber(file, caption string) (int, string, bool) {
	for _, src := range []struct{ name, text string }{{"caption", caption}, {"filename", file}} {
		if match := seasonlessEpisodePattern.FindStringSubmatch(src.text); len(match) == 2 {
			n, err := strconv.Atoi(match[1])
			if err == nil && n > 0 && n <= 999 {
				return n, src.name, true
			}
		}
	}
	return 0, "", false
}

func (a *API) singleSeasonEpisodeTitles(ctx context.Context, id int64) (int, map[int]string, error) {
	var show struct {
		Seasons []struct {
			Number int `json:"season_number"`
		} `json:"seasons"`
	}
	if err := a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d", id), url.Values{"language": {"es-ES"}}, &show); err != nil {
		return 0, nil, err
	}
	season := 0
	for _, s := range show.Seasons {
		if s.Number > 0 {
			if season != 0 {
				return 0, nil, nil
			}
			season = s.Number
		}
	}
	if season == 0 {
		return 0, nil, nil
	}
	var response struct {
		Episodes []struct {
			Number int    `json:"episode_number"`
			Name   string `json:"name"`
		} `json:"episodes"`
	}
	if err := a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d/season/%d", id, season), url.Values{"language": {"es-ES"}}, &response); err != nil {
		return 0, nil, err
	}
	titles := make(map[int]string)
	for _, ep := range response.Episodes {
		titles[ep.Number] = ep.Name
	}
	return season, titles, nil
}

// Plain episode numbers must lead the name, have exactly two digits, and
// be separated from a title. Never consume years, resolutions or dates.
var leadingEpisodePattern = regexp.MustCompile(`^\s*(\d{2})(?:[ ._-]+(.+)|$)`)

func leadingEpisodeNumber(file, caption string) (int, string, bool) {
	for _, src := range []struct{ name, text string }{{"caption", caption}, {"filename", strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))}} {
		match := leadingEpisodePattern.FindStringSubmatch(src.text)
		if len(match) != 3 {
			continue
		}
		tail := strings.TrimSpace(match[2])
		if tail != "" && tail[0] >= '0' && tail[0] <= '9' {
			continue
		}
		n, err := strconv.Atoi(match[1])
		if err == nil && n > 0 {
			return n, src.name, true
		}
	}
	return 0, "", false
}
