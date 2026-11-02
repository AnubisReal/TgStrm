package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var episodeFractionPattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(\d{1,3})[ ._-]+(?:de|of)[ ._-]+(\d{1,3})(?:[^[:alnum:]]|$)`)

func episodeFraction(file, caption string) (int, int, string, bool) {
	for _, src := range []struct{ name, text string }{{"caption", caption}, {"filename", file}} {
		match := episodeFractionPattern.FindStringSubmatch(src.text)
		if len(match) != 3 {
			continue
		}
		n, _ := strconv.Atoi(match[1])
		total, _ := strconv.Atoi(match[2])
		return n, total, src.name, n > 0 && total > 0 && n <= total
	}
	return 0, 0, "", false
}

func compactEpisodeCandidates(file, caption string) ([]episodeIdentity, bool) {
	for _, src := range []struct{ name, text string }{{"caption", caption}, {"filename", strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))}} {
		n, tokens := 0, 0
		for _, token := range strings.FieldsFunc(src.text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("_.-", r) }) {
			if len(token) != 3 {
				continue
			}
			value, err := strconv.Atoi(token)
			if err == nil && value >= 100 {
				n = value
				tokens++
			}
		}
		if tokens != 1 {
			continue
		}

		// The token alone cannot distinguish S01E01, S10E01, or episode 101.
		return []episodeIdentity{{Season: n / 100, Episode: n % 100, Source: src.name}, {Season: n / 10, Episode: n % 10, Source: src.name}, {Season: 1, Episode: n, Source: src.name}}, true
	}
	return nil, false
}

func uniqueCompactEpisode(candidates []episodeIdentity, exists func(int, int) bool) (episodeIdentity, bool) {
	var found episodeIdentity
	seen := map[[2]int]bool{}
	count := 0
	for _, ep := range candidates {
		key := [2]int{ep.Season, ep.Episode}
		if ep.Episode < 1 || seen[key] || !exists(ep.Season, ep.Episode) {
			continue
		}
		seen[key] = true
		found = ep
		count++
	}
	return found, count == 1
}

// Unlabelled three-digit tokens require the matched series as context.
func (a *API) detectSeriesEpisode(ctx context.Context, seriesID int64, file, caption string) (episodeIdentity, bool) {
	if ep, handled, ok := a.detectCumulativeEpisode(ctx, seriesID, file); handled {
		return ep, ok
	}
	if ep, ok := detectEpisode(file, caption); ok {
		return ep, true
	}
	number, total, source, fraction := episodeFraction(file, caption)
	candidates, ok := compactEpisodeCandidates(file, caption)
	if (!ok && !fraction) || seriesID <= 0 {
		return episodeIdentity{}, false
	}
	var show struct {
		Seasons []struct {
			Number int `json:"season_number"`
			Count  int `json:"episode_count"`
		} `json:"seasons"`
	}
	if a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d", seriesID), url.Values{"language": {"es-ES"}}, &show) != nil {
		return episodeIdentity{}, false
	}
	counts := map[int]int{}
	for _, s := range show.Seasons {
		counts[s.Number] = s.Count
	}
	if fraction {
		candidates = nil
		explicit := -1
		for _, text := range []string{caption, file} {
			if match := explicitSeasonPattern.FindStringSubmatch(text); len(match) == 2 {
				explicit, _ = strconv.Atoi(match[1])
				break
			}
		}
		for season, count := range counts {
			if season > 0 && count == total && (explicit < 0 || season == explicit) {
				candidates = append(candidates, episodeIdentity{Season: season, Episode: number, Source: source})
			}
		}
	}
	valid := map[[2]int]bool{}
	loaded := map[int]bool{}
	for _, ep := range candidates {
		if ep.Episode < 1 || ep.Episode > counts[ep.Season] || loaded[ep.Season] {
			continue
		}
		loaded[ep.Season] = true
		var season struct {
			Episodes []struct {
				Number int `json:"episode_number"`
			} `json:"episodes"`
		}
		if a.tmdbRequestContext(ctx, fmt.Sprintf("tv/%d/season/%d", seriesID, ep.Season), url.Values{"language": {"es-ES"}}, &season) != nil {
			return episodeIdentity{}, false
		}
		for _, e := range season.Episodes {
			valid[[2]int{ep.Season, e.Number}] = true
		}
	}
	return uniqueCompactEpisode(candidates, func(s, e int) bool { return valid[[2]int{s, e}] })
}
