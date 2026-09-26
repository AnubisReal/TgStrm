package httpapi

import (
	"strings"
	"testing"
)

func TestSplitTMDBTitleYear(t *testing.T) {
	tests := []struct{ input, title, year string }{
		{"Un talento único (2026)", "Un talento único", "2026"},
		{"Dallas Buyers Club [2013]", "Dallas Buyers Club", "2013"},
		{"Cold War 2018", "Cold War", "2018"},
		{"2001: Una odisea del espacio", "2001: Una odisea del espacio", ""},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			title, year := splitTMDBTitleYear(test.input)
			if title != test.title || year != test.year {
				t.Fatalf("got %q %q, want %q %q", title, year, test.title, test.year)
			}
		})
	}
}

func TestSplitTMDBTitleYearWithProviderID(t *testing.T) {
	title, year := splitTMDBTitleYear("El boom de los asteroides (2022) [tmdbid-12345]")
	if title != "El boom de los asteroides" || year != "2022" {
		t.Fatalf("unexpected split: %q %q", title, year)
	}
	clean, id, ok := splitTMDBIDMarker("El boom de los asteroides (2022) [tmdbid-12345]")
	if !ok || clean != "El boom de los asteroides (2022)" || id != 12345 {
		t.Fatalf("unexpected marker: %q %d %v", clean, id, ok)
	}
}

func TestCatalogTopicTitlePreservesProviderIDWithinTelegramLimit(t *testing.T) {
	got := catalogTopicTitle(tmdbResult{ID: 12345, Title: strings.Repeat("Título largo ", 20), ReleaseDate: "2022-01-01"})
	if len([]rune(got)) > 128 || !strings.HasSuffix(got, "[tmdbid-12345]") {
		t.Fatalf("invalid Telegram topic title: %q", got)
	}
}

func TestNormalizeTMDBType(t *testing.T) {
	for input, want := range map[string]string{"movie": "movie", "movies": "movie", "tv": "tv", "shows": "tv"} {
		got, ok := normalizeTMDBType(input)
		if !ok || got != want {
			t.Fatalf("normalize %q: got %q ok=%v", input, got, ok)
		}
	}
	if _, ok := normalizeTMDBType("person"); ok {
		t.Fatal("unsupported TMDB type accepted")
	}
}

func TestTMDBSourcePrefersCaptionAndCleansTechnicalTags(t *testing.T) {
	got := tmdbSource("The Palace (2023)\ntexto adicional", "The.Palace.2023.1080p.WEB-DL.mp4")
	if got != "The Palace (2023)" {
		t.Fatalf("unexpected source: %q", got)
	}
	got = tmdbSource("", "Cold War (2018) 1080p BluRay.mp4")
	if got != "Cold War (2018)" {
		t.Fatalf("unexpected filename source: %q", got)
	}
}

func TestTMDBExactMatchRequiresTitleAndRequestedYear(t *testing.T) {
	results := []tmdbResult{{ID: 1, Title: "Cold War", ReleaseDate: "2018-06-08"}, {ID: 2, Title: "Cold War", ReleaseDate: "2012-01-01"}}
	match, ok := tmdbExactMatch("Cold War (2018)", results)
	if !ok || match.ID != 1 {
		t.Fatalf("expected the exact 2018 match, got %+v ok=%v", match, ok)
	}
	if _, ok = tmdbExactMatch("Cold War (2020)", results); ok {
		t.Fatal("accepted a title with the wrong requested year")
	}
}
