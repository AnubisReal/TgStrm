package httpapi

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"tgstrm/internal/library"
)

var referenceTVID = regexp.MustCompile(`^/tv/([0-9]+)(?:-|/|$)`)
var referenceIMDbID = regexp.MustCompile(`^/title/(tt[0-9]+)(?:/|$)`)
var referenceSeriesNoise = regexp.MustCompile(`(?i)\s*\((?:serie de tv|tv series|miniserie de tv|miniserie|serie)\)`)
var referenceProviderSuffix = regexp.MustCompile(`(?i)\s*[-–|]\s*filmaffinity\s*$`)
var referenceYear = regexp.MustCompile(`\((19[0-9]{2}|20[0-9]{2})\)`)

func referenceTitle(value string) string {
	value = referenceProviderSuffix.ReplaceAllString(value, "")
	return strings.TrimSpace(referenceSeriesNoise.ReplaceAllString(value, ""))
}

// A URL identifier or a unique exact title/year is evidence; popularity alone is not.
func (a *API) matchReplicationReferences(ctx context.Context, refs []library.TopicReference) (tmdbResult, bool, bool) {
	var chosen tmdbResult
	accept := func(match tmdbResult) bool {
		if chosen.ID != 0 && chosen.ID != match.ID {
			return false
		}
		chosen = match
		return true
	}
	for _, ref := range refs {
		parsed, err := url.Parse(ref.URL)
		if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		var match tmdbResult
		var ok bool
		switch strings.ToLower(parsed.Hostname()) {
		case "themoviedb.org", "www.themoviedb.org":
			if parts := referenceTVID.FindStringSubmatch(parsed.Path); len(parts) == 2 {
				id, _ := strconv.ParseInt(parts[1], 10, 64)
				match, err = a.localizedTVDetails(ctx, id)
				ok = err == nil && match.ID > 0
			}
		case "imdb.com", "www.imdb.com":
			if parts := referenceIMDbID.FindStringSubmatch(parsed.Path); len(parts) == 2 {
				var result struct {
					TV []struct {
						ID int64 `json:"id"`
					} `json:"tv_results"`
				}
				err = a.tmdbRequestContext(ctx, "find/"+parts[1], url.Values{"external_source": {"imdb_id"}, "language": {"es-ES"}}, &result)
				if err == nil && len(result.TV) == 1 {
					match, err = a.localizedTVDetails(ctx, result.TV[0].ID)
					ok = err == nil && match.ID > 0
				}
			}
		case "filmaffinity.com", "www.filmaffinity.com":
			title := referenceTitle(ref.Title)
			// An unlabeled FilmAffinity URL cannot be mapped to TMDB. Require the year too.
			if referenceYear.MatchString(title) {
				results, searchErr := a.searchExternalSeries(ctx, title)
				if searchErr == nil {
					match, ok = uniqueReplicationMatch(title, results)
				}
				if ok {
					match, err = a.localizedTVDetails(ctx, match.ID)
					ok = err == nil
				}
			}
		}
		if ok && !accept(match) {
			return tmdbResult{}, false, true
		}
	}
	return chosen, chosen.ID > 0, false
}

func uniqueReplicationMatch(source string, results []tmdbResult) (tmdbResult, bool) {
	var chosen tmdbResult
	for _, result := range results {
		if match, ok := tmdbExactMatch(source, []tmdbResult{result}); ok {
			if chosen.ID != 0 && chosen.ID != match.ID {
				return tmdbResult{}, false
			}
			chosen = match
		}
	}
	return chosen, chosen.ID > 0
}
