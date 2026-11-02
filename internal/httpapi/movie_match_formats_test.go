package httpapi

import (
	"slices"
	"testing"
)

func TestMovieAdditionalFormats(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2025 - Ahora Me Ves 3 (Now You See Me꞉ Now You Don't).mkv", "Now You See Me꞉ Now You Don't (2025)"},
		{"Allanamiento De Morada (Home Invasion) (2016) (Spa-Eng).mkv", "Home Invasion (2016)"},
		{"Plácido (Luis García Berlanga, 1961) [HD 1080p VO].mkv", "Plácido (1961)"},
		{"2001-Salvajes 1080p y sub.mkv", "Salvajes (2001)"},
		{"La_Última_Casa_20261080p_WEBRip_x264.mkv", "La Última Casa (2026)"},
		{"2001 A Space Odyssey.mkv", "2001 A Space Odyssey"},
	} {
		if got := additionalMovieSources("", tc.in); !slices.Contains(got, tc.want) {
			t.Errorf("%s: %v missing %s", tc.in, got, tc.want)
		}
	}
}
func TestMovieAliasesRequireWholeIdentity(t *testing.T) {
	names := []string{"La larga marcha", "The Long Walk"}
	for _, tc := range []struct {
		source string
		ok     bool
	}{{"La larga marcha (2025)", true}, {"La larga marcha The Long Walk (2025)", true}, {"La larga marcha otra película (2025)", false}, {"La larga (2025)", false}} {
		if movieAliasMatches(tc.source, names) != tc.ok {
			t.Error(tc)
		}
	}
	if !movieAliasMatches("Los crímenes de Fjallbacka", []string{"Los crímenes de Fjällbacka"}) {
		t.Fatal("diacritics")
	}
	if !movieAliasMatches("Amsterdamned 2", []string{"Amsterdamned II"}) {
		t.Fatal("roman sequel")
	}
	if !movieAliasMatches("The Descent Parte 2", []string{"The Descent: Part II"}) {
		t.Fatal("localized part marker")
	}
	if !movieAliasMatches("KungFury", []string{"Kung Fury"}) {
		t.Fatal("joined title")
	}
	if movieAliasMatches("Rocky 3", []string{"Rocky II"}) {
		t.Fatal("distinct sequels")
	}
}
func TestMovieExplicitIdentityConflicts(t *testing.T) {
	for _, tc := range []struct {
		body, file string
		id         int64
		imdb       string
		conflict   bool
	}{
		{"[tmdb-38780]", "", 38780, "", false}, {"", "Rampage_tmdb_38780.mp4", 38780, "", false},
		{"https://www.themoviedb.org/movie/38780-rampage", "", 38780, "", false},
		{"[tt5215952]", "", 0, "tt5215952", false},
		{"[tmdb-1]", "movie_tmdb_2.mp4", 0, "", true},
		{"[tt1234567]", "tt7654321.mkv", 0, "", true},
		{"https://www.themoviedb.org/tv/38780", "", 0, "", false},
	} {
		id, imdb, conflict := movieExplicitIdentifiers(tc.body, tc.file)
		if id != tc.id || imdb != tc.imdb || conflict != tc.conflict {
			t.Errorf("%+v: %d %s %v", tc, id, imdb, conflict)
		}
	}
}
func TestMovieCreditSuffixRequiresProviderEvidence(t *testing.T) {
	if !movieCreditSuffix("Bright David Ayer (2017)", []string{"Bright"}, []string{"David Ayer"}) {
		t.Fatal("verified director")
	}
	if movieCreditSuffix("Bright Unknown Person (2017)", []string{"Bright"}, []string{"David Ayer"}) {
		t.Fatal("unknown credit accepted")
	}
	if movieCreditSuffix("Bright Future (2017)", []string{"Bright"}, []string{"David Ayer"}) {
		t.Fatal("different title truncated")
	}
	if !movieCreditSuffix("La entrevista Evan Goldberg Seth Rogen (2014)", []string{"La entrevista"}, []string{"Evan Goldberg", "Seth Rogen"}) {
		t.Fatal("two directors")
	}
	if !slices.Contains(additionalMovieSources("", "La_carrera_de_la_muerte_del_año_2000_Paul_Bartel,_1975.mkv"), "La carrera de la muerte del año 2000 Paul Bartel (1975)") {
		t.Fatal("title year confused with release year")
	}
}
