package httpapi

import "testing"

func TestCorrectedSeriesCaptionUsesNewSeriesAndEpisode(t *testing.T) {
	cases := []struct {
		name               string
		episode            episodeIdentity
		identified         bool
		episodeTitle, want string
	}{
		{"episode", episodeIdentity{Season: 1, Episode: 2}, true, "Nuevo título", "Serie correcta (2024) S01E02 Nuevo título"},
		{"unidentified", episodeIdentity{}, false, "Título antiguo", "Serie correcta (2024)"},
		{"multiple", episodeIdentity{Season: 2, Episode: 3, EndEpisode: 4}, true, "Título antiguo", "Serie correcta (2024) S02E03-E04 Episodios 03-04"},
		{"no title", episodeIdentity{Season: 0, Episode: 1}, true, "", "Serie correcta (2024) S00E01"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := correctedSeriesCaption("Serie correcta (2024)", c.episode, c.identified, c.episodeTitle)
			if got != c.want {
				t.Fatalf("%q != %q", got, c.want)
			}
		})
	}
}

func TestSeasonlessEpisodeReadsOnlyExplicitEpisodeLabel(t *testing.T) {
	for _, c := range []struct {
		file, caption string
		want          int
		ok            bool
	}{
		{"Animal Episodio 9.mkv", "", 9, true},
		{"video.mkv", "Episode 02", 2, true},
		{"Animal (2025).mkv", "", 0, false},
		{"Animal 1080p.mkv", "", 0, false},
		{"Capítulo 0.mkv", "", 0, false},
	} {
		n, _, ok := seasonlessEpisodeNumber(c.file, c.caption)
		if n != c.want || ok != c.ok {
			t.Fatalf("%q: %d %v", c.file, n, ok)
		}
	}
}
