package httpapi

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	telegramauth "tgstrm/internal/telegram"
)

func TestDetectEpisodePrefersCaptionThenFilename(t *testing.T) {
	tests := []struct {
		file, caption   string
		season, episode int
		source          string
	}{
		{"Arrow.S02E03.1080p.mkv", "S09E09", 9, 9, "caption"},
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

func TestMovieJellyfinVersions(t *testing.T) {
	first, ok := movieProfileSTRMPath("jellyfin_tmdb", "Movie", "2024", 123, "asset-a", true)
	second, _ := movieProfileSTRMPath("jellyfin_tmdb", "Movie", "2024", 123, "asset-b", true)
	if !ok || first == second || filepath.ToSlash(first) != "Movie (2024) [tmdbid-123]/Movie (2024) [tmdbid-123] - Version asset-a.strm" {
		t.Fatalf("invalid grouped versions: %q / %q", first, second)
	}
	legacy, _ := movieSTRMPath("Movie", "2024")
	universal, _ := movieProfileSTRMPath("universal", "Movie", "2024", 123, "asset-a", true)
	if legacy != universal {
		t.Fatal("universal paths changed")
	}
}

func TestDetectMultiEpisodeRanges(t *testing.T) {
	for _, name := range []string{"Show S01E01-E02.mkv", "Show S01E01-02.mkv", "Show 1x01-02.mkv", "Show S01E01-S01E02.mkv"} {
		episode, ok := detectEpisode(name, "")
		if !ok || episode.Season != 1 || episode.Episode != 1 || episode.lastEpisode() != 2 || episode.code() != "S01E01-E02" {
			t.Fatalf("range lost: %q %#v", name, episode)
		}
		path := seriesSTRMPath("universal", "Show", "2024", 123, episode, "Double episode", false)
		if !strings.Contains(path, "S01E01-E02") {
			t.Fatalf("range lost in path: %s", path)
		}
	}
	episode, ok := detectEpisode("video.mkv", "Show S02E03-E04")
	if !ok || episode.Source != "caption" || episode.lastEpisode() != 4 {
		t.Fatal("caption range not detected")
	}
	for _, name := range []string{"S01E02-E01.mkv", "S01E01-S02E02.mkv", "S01E01-E999.mkv"} {
		if episode, ok := detectEpisode(name, ""); ok {
			t.Fatalf("invalid range accepted: %s %#v", name, episode)
		}
	}
}

func TestTopicEpisodeRangeEnrichment(t *testing.T) {
	messages := []telegramauth.TopicMessage{
		{FileName: "Show S01E01-E02.mkv"},
		{FileName: "video.mkv", Body: "Show S02E03-E04"},
		{FileName: "Show S01E01-E02.mkv", SeasonNumber: 3, EpisodeNumber: 7},
	}
	enrichTopicEpisodeNumbers(messages)
	if messages[0].EpisodeEndNumber != 2 || messages[0].EpisodeSource != "filename" || messages[1].EpisodeEndNumber != 4 || messages[1].EpisodeSource != "caption" {
		t.Fatalf("missing enrichment: %#v", messages)
	}
	if messages[2].SeasonNumber != 3 || messages[2].EpisodeNumber != 7 || messages[2].EpisodeEndNumber != 0 {
		t.Fatal("confirmed assignment overwritten")
	}
}

func TestOptionalTMDBFolderID(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		movie, ok := movieProfileSTRMPath("jellyfin_tmdb", "Movie", "2024", 123, "asset-a", enabled)
		series := seriesSTRMPath("jellyfin_tmdb", "Show", "2024", 456, episodeIdentity{Season: 1, Episode: 1}, "Pilot", enabled)
		if !ok || strings.Contains(movie, "[tmdbid-") != enabled || strings.Contains(series, "[tmdbid-") != enabled {
			t.Fatalf("unexpected ID naming: %q %q", movie, series)
		}
		if !enabled && filepath.Dir(movie) != "Movie (2024)" {
			t.Fatal(movie)
		}
	}
}

func TestEpisodeDetectionComplementsFilenameAndCaption(t *testing.T) {
	for _, c := range []struct {
		file, caption   string
		season, episode int
	}{
		{"Animal Episodio 1.mkv", "", 1, 1},
		{"", "Animal Episodio 9", 1, 9},
		{"video.mkv", "Serie S02E03", 2, 3},
		{"Serie S03E04.mkv", "Sin información", 3, 4},
		{"Serie S03E04.mkv", "Serie S02E03", 2, 3},
		{"Episodio 4.mkv", "Temporada 2", 2, 4},
		{"Season 3 video.mkv", "Episode 7", 3, 7},
		{"Serie S02E08.mkv", "Episodio 1", 2, 8},
	} {
		ep, ok := detectEpisode(c.file, c.caption)
		if !ok || ep.Season != c.season || ep.Episode != c.episode {
			t.Fatalf("%q / %q: %+v %v", c.file, c.caption, ep, ok)
		}
	}
	for _, file := range []string{"Animal 2025.mkv", "Video 1080p.mkv", "Episodio 0.mkv"} {
		if ep, ok := detectEpisode(file, ""); ok {
			t.Fatalf("unexpected identity: %s %+v", file, ep)
		}
	}
}

func TestLeadingEpisodeNumberComplementsExistingFormats(t *testing.T) {
	for _, c := range []struct {
		file, caption   string
		season, episode int
	}{
		{"01 Fiesta - años universitarios -VCS.avi", "Undergrads (2001)", 1, 1},
		{"03_Virgenes_años_universitarios.avi", "", 1, 3},
		{"13_Faltan_72_horas_para_que_termine.avi", "", 1, 13},
		{"01.avi", "", 1, 1},
		{"", "02 Tradiciones", 1, 2},
		{"03_video.avi", "S02E07", 2, 7},
		{"03_video.avi", "Temporada 2", 2, 3},
		{"S03E04.avi", "01 Fiesta", 3, 4},
	} {
		ep, ok := detectEpisode(c.file, c.caption)
		if !ok || ep.Season != c.season || ep.Episode != c.episode {
			t.Fatalf("%q/%q: %+v %v", c.file, c.caption, ep, ok)
		}
	}
	for _, name := range []string{"2001 Serie.avi", "1080p.avi", "00_video.avi", "01-02-2024.avi", "Serie 03.avi", "123_video.avi"} {
		if ep, ok := detectEpisode(name, ""); ok {
			t.Fatalf("unexpected identity %q %+v", name, ep)
		}
	}
}

func TestCaptionOnlyAliasElMexicanoEpisodes(t *testing.T) {
	for n := 1; n <= 78; n++ {
		for _, caption := range []string{fmt.Sprintf("Alias el Mexicano Capitulo %d", n), fmt.Sprintf("Alias el Mexicano Capitulo%d", n), fmt.Sprintf("Alias el Mexicano - Capítulo %d final", n)} {
			ep, ok := detectEpisode("", caption)
			if !ok || ep.Season != 1 || ep.Episode != n {
				t.Fatalf("%q: %+v %v", caption, ep, ok)
			}
		}
	}
}

func TestAbbreviatedCapEpisodeDetection(t *testing.T) {
	if _, _, ok := seasonlessEpisodeNumber("Cap 2024.mp4", ""); ok {
		t.Fatal("year accepted as a seasonless episode")
	}
	for _, c := range []struct {
		file, caption   string
		season, episode int
		source          string
	}{
		{"Cap 1.mp4", "Ashley Madison (2024)", 1, 1, "filename"},
		{"Cap 2.mp4", "", 1, 2, "filename"},
		{"", "Cap 3", 1, 3, "caption"},
		{"video.mp4", "Serie Cap. 03", 1, 3, "caption"},
		{"Cap_04.mkv", "Temporada 2", 2, 4, "filename"},
		{"Cap 1.mp4", "Serie S03E02", 3, 2, "caption"},
	} {
		ep, ok := detectEpisode(c.file, c.caption)
		if !ok || ep.Season != c.season || ep.Episode != c.episode || ep.Source != c.source {
			t.Fatalf("%q / %q: %+v %v", c.file, c.caption, ep, ok)
		}
	}
	for _, file := range []string{"Cap 0.mp4", "Capacidad 3.mp4", "Recap 2.mp4"} {
		if ep, ok := detectEpisode(file, ""); ok {
			t.Fatalf("false positive %q: %+v", file, ep)
		}
	}
}

func TestSeasonChapterCFormat(t *testing.T) {
	for _, input := range []string{"Aida T9 C01.mp4", "Aida T10 C46.mp4", "Aida T11 C38.mp4"} {
		ep, ok := detectEpisode(input, "")
		if !ok || ep.Season < 9 || ep.Episode < 1 {
			t.Fatalf("%s: %+v", input, ep)
		}
		captionEp, ok := detectEpisode("", input)
		if !ok || captionEp != (episodeIdentity{Season: ep.Season, Episode: ep.Episode, Source: "caption"}) {
			t.Fatal(captionEp)
		}
	}
}

func TestCumulativeTitleMatchRequiresUniqueEvidence(t *testing.T) {
	entries := []seasonTitleEntry{{1, "Aerobiza como puedas"}, {8, "No es país para pijas"}}
	if n, ok := uniqueSeasonTitle("No es pais para pijas", entries); !ok || n != 8 {
		t.Fatal(n, ok)
	}
	for _, title := range []string{"Especial mejores momentos", "Paz la desconocida", ""} {
		if _, ok := uniqueSeasonTitle(title, entries); ok {
			t.Fatal(title)
		}
	}
	entries = append(entries, seasonTitleEntry{9, "No es país para pijas"})
	if _, ok := uniqueSeasonTitle("No es país para pijas", entries); ok {
		t.Fatal("ambiguous title accepted")
	}
}
