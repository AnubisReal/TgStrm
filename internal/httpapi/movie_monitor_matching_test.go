package httpapi

import "testing"

func TestMovieAutomaticMatchRequiresUniqueIdentity(t *testing.T) {
	a := tmdbResult{ID: 10, Title: "Película", OriginalTitle: "Film", ReleaseDate: "2020-01-01"}
	b := tmdbResult{ID: 11, Title: "Película", ReleaseDate: "2020-08-01"}
	if _, ok := tmdbExactMatch("Película (2020)", []tmdbResult{a, b}); ok {
		t.Fatal("same title and year with different IDs must require review")
	}
	if got, ok := tmdbExactMatch("Film (2020)", []tmdbResult{a, b}); !ok || got.ID != a.ID {
		t.Fatal("unique original title must remain supported")
	}
	if got, ok := tmdbExactMatch("Película (2020)", []tmdbResult{a, a}); !ok || got.ID != a.ID {
		t.Fatal("repeated evidence for one ID must not create ambiguity")
	}
	b.ReleaseDate = "2021-08-01"
	if got, ok := tmdbExactMatch("Película (2020)", []tmdbResult{a, b}); !ok || got.ID != a.ID {
		t.Fatal("year must distinguish releases")
	}
	if _, ok := tmdbExactMatch("Película", []tmdbResult{a, b}); ok {
		t.Fatal("missing year must not select the first remake")
	}
}
