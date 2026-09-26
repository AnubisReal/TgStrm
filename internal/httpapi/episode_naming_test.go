package httpapi

import "testing"

func TestDetectEpisodePrefersFilenameThenCaption(t *testing.T) {
	tests := []struct {
		file, caption   string
		season, episode int
		source          string
	}{
		{"Arrow.S02E03.1080p.mkv", "S09E09", 2, 3, "filename"},
		{"video_8372.mkv", "Arrow — Temporada 3 Capítulo 7", 3, 7, "caption"},
		{"show.1x09.mkv", "", 1, 9, "filename"},
		{"archivo.mkv", "T02 E05", 2, 5, "caption"},
		{"archivo.mkv", "[Cap.805]", 8, 5, "caption"},
	}
	for _, test := range tests {
		got, ok := detectEpisode(test.file, test.caption)
		if !ok || got.Season != test.season || got.Episode != test.episode || got.Source != test.source {
			t.Fatalf("detectEpisode(%q,%q) = %#v,%v", test.file, test.caption, got, ok)
		}
	}
}

func TestDetectEpisodeRejectsAmbiguousNumbers(t *testing.T) {
	if got, ok := detectEpisode("Arrow 2024 1080p.mkv", "Publicado 12-10-2024"); ok {
		t.Fatalf("unexpected episode: %#v", got)
	}
}
