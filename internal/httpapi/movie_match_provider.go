package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *API) identifyMonitoredMovie(ctx context.Context, body, file string) (tmdbResult, bool, error) {
	if monitoredMovieNeedsManualMediaReview(body, file) {
		return tmdbResult{}, false, nil
	}
	id, imdb, conflict := movieExplicitIdentifiers(body, file)
	if conflict {
		return tmdbResult{}, false, nil
	}
	if id > 0 || imdb != "" {
		var result tmdbResult
		if id > 0 {
			if err := a.tmdbRequestContext(ctx, fmt.Sprintf("movie/%d", id), url.Values{"language": {"es-ES"}}, &result); err != nil {
				return tmdbResult{}, false, err
			}
			if result.ID != id {
				return tmdbResult{}, false, nil
			}
		}
		if imdb != "" {
			var response struct {
				Movies []tmdbResult `json:"movie_results"`
			}
			if err := a.tmdbRequestContext(ctx, "find/"+imdb, url.Values{"external_source": {"imdb_id"}, "language": {"es-ES"}}, &response); err != nil {
				return tmdbResult{}, false, err
			}
			if len(response.Movies) != 1 || (result.ID > 0 && result.ID != response.Movies[0].ID) {
				return tmdbResult{}, false, nil
			}
			result = response.Movies[0]
		}
		return result, result.ID > 0, nil
	}
	// Preserve all established exact matching behavior before adding new evidence.
	old, ok, err := a.identifyMonitoredMovieLegacy(ctx, body, file)
	if err != nil || ok {
		return old, ok, err
	}
	sources := append([]string{cleanMonitoredMovieSource(body), cleanMonitoredMovieSource(file)}, additionalMovieSources(body, file)...)
	type candidate struct {
		r             tmdbResult
		aliases       []string
		loaded        bool
		years         map[string]bool
		directors     []string
		creditsLoaded bool
		unavailable   bool
	}
	candidates := map[int64]*candidate{}
	searches := map[string][]tmdbResult{}
	checked := map[string]bool{}
	var found tmdbResult
	accept := func(r tmdbResult) bool {
		if found.ID > 0 && found.ID != r.ID {
			return false
		}
		found = r
		return true
	}
	requests := 0
	uncertain := false
	aliasLookups := 0
	for _, source := range sources {
		title, year := splitTMDBTitleYear(source)
		key := movieTitleFold(source) + "|" + year
		if title == "" || len(source) > 180 || checked[key] {
			continue
		}
		checked[key] = true
		// Prefixes retrieve bilingual titles, but never constitute matching evidence.
		queries := []string{title}
		words := strings.Fields(title)
		if len(words) > 4 {
			queries = append(queries, strings.Join(words[:4], " "))
		}
		if len(words) > 2 {
			queries = append(queries, strings.Join(words[:2], " "))
		}
		if len(words) > 2 && len(words[0]) >= 5 {
			queries = append(queries, words[0])
		}
		sourceFound := false
		for _, query := range queries {
			searchKey := query + "|" + year
			results, exists := searches[searchKey]
			if !exists {
				if requests >= 12 {
					uncertain = true
					continue
				}
				requests++
				var response struct {
					Results []tmdbResult `json:"results"`
					Total   int          `json:"total_results"`
				}
				if err := a.tmdbRequestContext(ctx, "search/movie", url.Values{"query": {query}, "language": {"es-ES"}, "include_adult": {"false"}}, &response); err != nil {
					return tmdbResult{}, false, err
				}
				// Do not silently choose from a truncated provider result set.
				if response.Total > 20 && year != "" && requests < 12 {
					requests++
					if err := a.tmdbRequestContext(ctx, "search/movie", url.Values{"query": {query}, "language": {"es-ES"}, "include_adult": {"false"}, "primary_release_year": {year}}, &response); err != nil {
						return tmdbResult{}, false, err
					}
				}
				if response.Total > 20 {
					searches[searchKey] = nil
					continue
				}
				results = response.Results
				searches[searchKey] = results
			}
			for _, r := range results {
				if r.ID <= 0 {
					continue
				}
				if year != "" && len(r.ReleaseDate) >= 4 {
					expected, _ := strconv.Atoi(year)
					actual, _ := strconv.Atoi(r.ReleaseDate[:4])
					if actual < expected-2 || actual > expected+2 {
						continue
					}
				}
				c := candidates[r.ID]
				if c == nil {
					c = &candidate{r: r, aliases: []string{r.Title, r.OriginalTitle}}
					candidates[r.ID] = c
				}
				if c.unavailable {
					continue
				}
				matches := movieAliasMatches(source, c.aliases)
				if !matches && !c.loaded {
					// At most six distinct alias lookups per file; a broad query stays manual.
					if aliasLookups >= 6 {
						uncertain = true
						continue
					}
					aliasLookups++
					var aliases struct {
						Titles []struct {
							Title string `json:"title"`
						} `json:"titles"`
					}
					if err := a.tmdbRequestContext(ctx, fmt.Sprintf("movie/%d/alternative_titles", r.ID), nil, &aliases); err != nil {
						if movieTMDBResourceMissing(err) {
							c.unavailable = true
							continue
						}
						return tmdbResult{}, false, err
					}
					for _, v := range aliases.Titles {
						c.aliases = append(c.aliases, v.Title)
					}
					c.loaded = true
					matches = movieAliasMatches(source, c.aliases)
				}
				if !matches && movieHasCreditSuffix(source, c.aliases) {
					if !c.creditsLoaded {
						var credits struct {
							Crew []struct {
								Name string `json:"name"`
								Job  string `json:"job"`
							} `json:"crew"`
						}
						if err := a.tmdbRequestContext(ctx, fmt.Sprintf("movie/%d/credits", r.ID), nil, &credits); err != nil {
							if movieTMDBResourceMissing(err) {
								c.unavailable = true
								continue
							}
							return tmdbResult{}, false, err
						}
						for _, person := range credits.Crew {
							if person.Job == "Director" {
								c.directors = append(c.directors, person.Name)
							}
						}
						c.creditsLoaded = true
					}
					matches = movieCreditSuffix(source, c.aliases, c.directors)
				}
				if !matches {
					continue
				}
				validYear := year == "" || strings.HasPrefix(r.ReleaseDate, year)
				if !validYear {
					if c.years == nil {
						var dates struct {
							Results []struct {
								Country string `json:"iso_3166_1"`
								Dates   []struct {
									Date string `json:"release_date"`
								} `json:"release_dates"`
							} `json:"results"`
						}
						if err := a.tmdbRequestContext(ctx, fmt.Sprintf("movie/%d/release_dates", r.ID), nil, &dates); err != nil {
							if movieTMDBResourceMissing(err) {
								c.unavailable = true
								continue
							}
							return tmdbResult{}, false, err
						}
						c.years = map[string]bool{}
						for _, region := range dates.Results {
							if region.Country == "ES" {
								for _, date := range region.Dates {
									if len(date.Date) >= 4 {
										c.years[date.Date[:4]] = true
									}
								}
							}
						}
					}
					validYear = c.years[year]
				}
				if validYear {
					if !accept(r) {
						return tmdbResult{}, false, nil
					}
					sourceFound = true
				}
			}
			if sourceFound {
				break
			}
		}
	}
	if uncertain {
		return tmdbResult{}, false, nil
	}
	return found, found.ID > 0, nil
}

// A missing candidate resource is permanent; transport and rate-limit failures are not.
func movieTMDBResourceMissing(err error) bool {
	var provider tmdbResponseError
	return errors.As(err, &provider) && provider.status == http.StatusNotFound
}
