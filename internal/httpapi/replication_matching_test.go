package httpapi

import "testing"

func TestFilmAffinityPreviewTitle(t *testing.T) {
	for input, want := range map[string]string{"ALF (Serie de TV) (1986) - FilmAffinity": "ALF (1986)", "The Following (TV Series) (2013) | FilmAffinity": "The Following (2013)", "Aída (2005)": "Aída (2005)"} {
		if got := referenceTitle(input); got != want {
			t.Fatalf("%q: %q != %q", input, got, want)
		}
	}
}
func TestReplicationMatchRejectsAmbiguousSeries(t *testing.T) {
	matches := []tmdbResult{{ID: 1, Title: "Example", ReleaseDate: "2000-01-01"}, {ID: 2, Title: "Example", ReleaseDate: "2020-01-01"}}
	if _, ok := uniqueReplicationMatch("Example", matches); ok {
		t.Fatal("ambiguous title automatically associated")
	}
	if match, ok := uniqueReplicationMatch("Example (2020)", matches); !ok || match.ID != 2 {
		t.Fatal("year did not disambiguate")
	}
	matches = append(matches, matches[1])
	if _, ok := uniqueReplicationMatch("Example (2020)", matches); !ok {
		t.Fatal("duplicate identical result treated as ambiguous")
	}
}
