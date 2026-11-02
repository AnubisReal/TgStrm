package httpapi

import "testing"

func TestCompactEpisodeRequiresUnambiguousSeriesContext(t *testing.T) {
	candidates, ok := compactEpisodeCandidates("Alias Charlie Sheen_101.mkv", "Alias Charlie Sheen (2025)")
	if !ok {
		t.Fatal("missing compact token")
	}
	for _, c := range []struct {
		valid           map[[2]int]bool
		season, episode int
		ok              bool
	}{
		{map[[2]int]bool{{1, 1}: true, {1, 2}: true}, 1, 1, true},
		{map[[2]int]bool{{10, 1}: true}, 10, 1, true},
		{map[[2]int]bool{{1, 101}: true}, 1, 101, true},
		{map[[2]int]bool{{1, 1}: true, {10, 1}: true}, 0, 0, false},
		{map[[2]int]bool{}, 0, 0, false},
	} {
		ep, ok := uniqueCompactEpisode(candidates, func(s, e int) bool { return c.valid[[2]int{s, e}] })
		if ok != c.ok || (ok && (ep.Season != c.season || ep.Episode != c.episode)) {
			t.Fatalf("%+v %v", ep, ok)
		}
	}
	for _, file := range []string{"Serie 2025.mkv", "Serie 1080p.mkv", "Serie 101 102.mkv"} {
		if _, ok := compactEpisodeCandidates(file, ""); ok {
			t.Fatalf("unexpected token %s", file)
		}
	}
	if _, ok := compactEpisodeCandidates("", "101"); !ok {
		t.Fatal("caption-only not detected")
	}
}

func TestEpisodeFractionRequiresSeriesValidation(t *testing.T) {
	for _, c := range []struct {
		file, caption string
		n, total      int
	}{
		{"Apocalipsis_Stalin_01_de_03_El_demonio.mkv", "", 1, 3},
		{"", "02 de 03 Rojo", 2, 3},
		{"03_of_03.mkv", "", 3, 3},
	} {
		n, total, _, ok := episodeFraction(c.file, c.caption)
		if !ok || n != c.n || total != c.total {
			t.Fatalf("%+v: %d %d %v", c, n, total, ok)
		}
		if _, ok := detectEpisode(c.file, c.caption); ok {
			t.Fatal("fraction accepted without TMDB")
		}
	}
	for _, file := range []string{"00_de_03.mkv", "04_de_03.mkv", "01_de_00.mkv"} {
		if _, _, _, ok := episodeFraction(file, ""); ok {
			t.Fatal(file)
		}
	}
	if ep, ok := detectEpisode("01_de_03.mkv", "S02E07"); !ok || ep.Season != 2 || ep.Episode != 7 {
		t.Fatal("explicit identity lost")
	}
}
