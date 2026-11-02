package httpapi

import (
	"regexp"
	"strings"
)

var movieYearToken = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)[0-9]{2})(?:[^0-9]|$)`)
var movieVideoSuffix = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|mov|m4v|webm|wmv|flv|ts|m2ts|mpeg|mpg)$`)
var movieTechnicalTail = regexp.MustCompile(`(?i)(?:^|\s)[\[(]?(?:m?\d{3,4}p\w*|[248]k|blu[ .-]?ray\w*|bd[ .-]?rip|br[ .-]?rip|web[ .-]?dl|web[ .-]?rip|hd[ .-]?rip|dvd[ .-]?rip|x26[45]|h26[45]|hevc|dual|triaudio|spanish|castellano|latino|vose)(?:\b|[^a-z]).*$`)
var movieSynopsisGap = regexp.MustCompile(`\s{4,}`)
var movieEpisodeMarker = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s\d{1,2}e\d{1,3}|\d{1,2}x\d{1,3})(?:[^0-9]|$)`)
var movieSplitFile = regexp.MustCompile(`(?i)(?:[._](?:mkv|mp4|avi)|\.part)[._]?[0-9]{2,3}$`)

// Conservative additional evidence for monitored movies only. Never rewrites
// source captions or file names, and never guesses a year that isn't present.
func cleanMonitoredMovieSource(value string) string {
	value = strings.TrimSpace(strings.Split(value, "\n")[0])
	if gap := movieSynopsisGap.FindStringIndex(value); gap != nil {
		value = value[:gap[0]]
	}
	value = movieVideoSuffix.ReplaceAllString(value, "")
	value = strings.NewReplacer("_", " ", ".", " ").Replace(value)
	value = strings.TrimSpace(strings.TrimLeftFunc(value, func(r rune) bool {
		return !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r >= 0xc0 && r <= 0x2ff)
	}))
	year := ""
	if m := movieYearToken.FindStringSubmatchIndex(value); len(m) > 0 && m[2] > 0 {
		prefix := strings.Trim(value[:m[2]], " []()-,: ")
		if prefix != "" {
			year = value[m[2]:m[3]]
			value = prefix
		}
	}
	value = movieTechnicalTail.ReplaceAllString(value, "")
	value = strings.Trim(strings.Join(strings.Fields(value), " "), " []()-,: ")
	if value == "" || len(value) > 180 {
		return ""
	}
	if year != "" {
		value += " (" + year + ")"
	}
	return value
}
func monitoredMovieSources(body, file string) []string {
	return []string{tmdbSource(body, file), tmdbSource("", file), cleanMonitoredMovieSource(body), cleanMonitoredMovieSource(file)}
}
func monitoredMovieNeedsManualMediaReview(body, file string) bool {
	return movieSplitFile.MatchString(strings.TrimSpace(file)) || movieEpisodeMarker.MatchString(strings.ReplaceAll(file, "_", " ")) || movieEpisodeMarker.MatchString(strings.Split(body, "\n")[0])
}

func monitoredMovieExactMatch(source string, results []tmdbResult) (tmdbResult, bool) {
	fold := func(value string) string {
		return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u").Replace(normalizedTMDBTitle(value))
	}
	_, year := splitTMDBTitleYear(source)
	wanted := fold(source)
	var chosen tmdbResult
	for _, r := range results {
		if r.ID <= 0 || wanted == "" || (fold(r.Title) != wanted && fold(r.OriginalTitle) != wanted) || (year != "" && !strings.HasPrefix(r.ReleaseDate, year)) {
			continue
		}
		if chosen.ID > 0 && chosen.ID != r.ID {
			return tmdbResult{}, false
		}
		chosen = r
	}
	return chosen, chosen.ID > 0
}
