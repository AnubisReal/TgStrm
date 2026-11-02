package httpapi

import (
	"golang.org/x/text/unicode/norm"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var movieLeadingYear = regexp.MustCompile(`^((?:19|20)[0-9]{2})\s*[-–:]\s*(.+)$`)
var movieFusedYear = regexp.MustCompile(`((?:19|20)[0-9]{2})(?:1080|2160|720|480)p`)
var movieParenthetical = regexp.MustCompile(`\(([^()]*)\)`)
var movieBracketed = regexp.MustCompile(`\[([^\[\]]*)\]`)
var movieIMDbMarker = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(tt[0-9]{7,10})(?:[^0-9]|$)`)
var movieTMDBMarker = regexp.MustCompile(`(?i)(?:\[tmdb(?:id)?[-_ :]+([1-9][0-9]*)\]|(?:^|[^a-z0-9])tmdb[_-]([1-9][0-9]*)(?:[^0-9]|$)|https?://(?:www\.)?themoviedb\.org/movie/([1-9][0-9]*))`)

// The additional forms supplement the original parser; they never alter media.
func additionalMovieSources(body, file string) []string {
	var out []string
	add := func(title, year string) {
		title = strings.Trim(strings.Join(strings.Fields(title), " "), " -.,:[]()")
		if title != "" && len(title) <= 180 {
			if year != "" {
				title += " (" + year + ")"
			}
			out = append(out, title)
		}
	}
	for _, raw := range []string{body, file} {
		raw = strings.TrimSpace(strings.Split(raw, "\n")[0])
		if raw == "" {
			continue
		}
		raw = movieVideoSuffix.ReplaceAllString(raw, "")
		raw = strings.NewReplacer("_", " ", ".", " ").Replace(raw)
		raw = movieFusedYear.ReplaceAllString(raw, "${1} 1080p")
		leading := ""
		if m := movieLeadingYear.FindStringSubmatch(raw); len(m) > 0 {
			leading = m[1]
			raw = m[2]
		}
		years := movieYearToken.FindAllStringSubmatchIndex(raw, -1)
		if len(years) > 1 {
			last := years[len(years)-1]
			if last[2] > 0 {
				add(raw[:last[2]], raw[last[2]:last[3]])
			}
		}
		cleaned := cleanMonitoredMovieSource(raw)
		title, year := splitTMDBTitleYear(cleaned)
		if leading != "" {
			if year != "" && year != leading {
				continue
			}
			year = leading
		}
		if missing := strings.Count(title, "(") - strings.Count(title, ")"); missing > 0 && missing <= 3 {
			title += strings.Repeat(")", missing)
		}
		// Isolate translated/original titles in parentheses, excluding technical tags.
		title = movieBracketed.ReplaceAllStringFunc(title, func(s string) string {
			inner := strings.Trim(s, "[]")
			if movieTechnicalTail.MatchString(" " + inner) {
				return " "
			}
			return s
		})
		var aliases []string
		without := movieParenthetical.ReplaceAllStringFunc(title, func(s string) string {
			inner := strings.Trim(s, "()")
			if movieTechnicalTail.MatchString(" " + inner) {
				return " "
			}
			if movieYearToken.MatchString(inner) {
				return s
			}
			aliases = append(aliases, inner)
			return " "
		})
		add(without, year)
		for _, alias := range aliases {
			add(alias, year)
		}
		// Parenthesized credits with a year: "Plácido (director, 1961)".
		for _, m := range movieParenthetical.FindAllStringSubmatchIndex(raw, -1) {
			inner := raw[m[2]:m[3]]
			ym := movieYearToken.FindStringSubmatch(inner)
			if len(ym) > 0 && strings.Contains(inner, ",") {
				add(raw[:m[0]], ym[1])
			}
		}
	}
	return out
}
func movieTitleFold(value string) string {
	title := normalizedTMDBTitle(value)
	var b strings.Builder
	for _, r := range norm.NFD.String(title) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	fields := strings.Fields(b.String())
	for i, v := range fields {
		switch v {
		case "ii":
			fields[i] = "2"
		case "iii":
			fields[i] = "3"
		case "iv":
			fields[i] = "4"
		}
	}
	for i, v := range fields {
		if i+1 < len(fields) && (v == "parte" || v == "part") {
			if _, err := strconv.Atoi(fields[i+1]); err == nil {
				fields[i] = "part"
			}
		}
	}
	return strings.Join(fields, " ")
}
func movieExplicitIdentifiers(body, file string) (int64, string, bool) {
	tmdbIDs := map[int64]bool{}
	imdbIDs := map[string]bool{}
	for _, raw := range []string{body, file} {
		for _, m := range movieTMDBMarker.FindAllStringSubmatch(raw, -1) {
			for _, v := range m[1:] {
				if v != "" {
					id, err := strconv.ParseInt(v, 10, 64)
					if err == nil && id > 0 {
						tmdbIDs[id] = true
					}
				}
			}
		}
		for _, m := range movieIMDbMarker.FindAllStringSubmatch(raw, -1) {
			imdbIDs[strings.ToLower(m[1])] = true
		}
	}
	if len(tmdbIDs) > 1 || len(imdbIDs) > 1 {
		return 0, "", true
	}
	var id int64
	var imdb string
	for v := range tmdbIDs {
		id = v
	}
	for v := range imdbIDs {
		imdb = v
	}
	return id, imdb, false
}
func movieAliasMatches(source string, aliases []string) bool {
	wanted := movieTitleFold(source)
	if wanted == "" {
		return false
	}
	for _, alias := range aliases {
		folded := movieTitleFold(alias)
		if folded == wanted || (len(wanted) >= 6 && strings.ReplaceAll(folded, " ", "") == strings.ReplaceAll(wanted, " ", "")) {
			return true
		}
	}
	// Some uploaders concatenate the Spanish and original titles. Both halves
	// must be complete names from the same TMDB record; arbitrary tails fail.
	for _, left := range aliases {
		l := movieTitleFold(left)
		if l == "" {
			continue
		}
		for _, right := range aliases {
			r := movieTitleFold(right)
			if r != "" && wanted == l+" "+r {
				return true
			}
		}
	}
	return false
}

// Credits are discarded only when TMDB confirms the complete suffix as a
// director name. This is not a heuristic for truncating an arbitrary title.
func movieCreditSuffix(source string, aliases, directors []string) bool {
	wanted := movieTitleFold(source)
	for _, alias := range aliases {
		prefix := movieTitleFold(alias)
		if len(prefix) < 5 || !strings.HasPrefix(wanted, prefix+" ") {
			continue
		}
		tail := strings.TrimPrefix(wanted, prefix+" ")
		tail = strings.TrimPrefix(strings.TrimPrefix(tail, "de "), "by ")
		for _, name := range directors {
			n := movieTitleFold(name)
			if n != "" && tail == n {
				return true
			}
			for _, other := range directors {
				o := movieTitleFold(other)
				if n != "" && o != "" && (tail == n+" "+o || tail == n+" y "+o) {
					return true
				}
			}
		}
	}
	return false
}
func movieHasCreditSuffix(source string, aliases []string) bool {
	wanted := movieTitleFold(source)
	for _, alias := range aliases {
		prefix := movieTitleFold(alias)
		if len(prefix) >= 5 && strings.HasPrefix(wanted, prefix+" ") {
			tail := strings.TrimPrefix(wanted, prefix+" ")
			if n := len(strings.Fields(tail)); n >= 2 && n <= 8 {
				return true
			}
		}
	}
	return false
}
