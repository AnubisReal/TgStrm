package httpapi

import "testing"

func TestMonitoredMovieAdditionalSourceFormats(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"No_Mires_Arriba_2021_Spanish_English_Subs_Web_Dl_1080P_X264_Ac3.mkv", "No Mires Arriba (2021)"},
		{"Scary Movie 3 [2003] [BR] [1080] [AC3-6CH].mkv", "Scary Movie 3 (2003)"},
		{"Dios_Mío,_¡Los_Niños_Han_Vuelto!_2021WEB_DL_1080p.mkv", "Dios Mío, ¡Los Niños Han Vuelto! (2021)"},
		{"🇪🇦 Willow        Cuento medieval con brujas", "Willow"},
		{"Push (2009).mkv", "Push (2009)"},
		{"2001 A Space Odyssey.mkv", "2001 A Space Odyssey"},
		{"Incoming 2018 1080p TRiAudio.mkv", "Incoming (2018)"},
	} {
		if got := cleanMonitoredMovieSource(tc.in); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}
func TestMonitoredMovieUnsafeMediaRequiresReview(t *testing.T) {
	for _, file := range []string{"ARQ (2016).mkv.00", "Another_Heaven_mkv.03", "Secretos_de_un_matrimonio_1x05.mkv", "Serie.S01E02.mp4"} {
		if !monitoredMovieNeedsManualMediaReview("", file) {
			t.Error(file)
		}
	}
	if monitoredMovieNeedsManualMediaReview("", "Scary Movie 3 (2003).mkv") {
		t.Fatal("movie treated as episode")
	}
}
func TestMonitoredMovieAccentMatchingStillRejectsAmbiguity(t *testing.T) {
	r := tmdbResult{ID: 1, Title: "El día de la boda", ReleaseDate: "2005-01-01"}
	if _, ok := monitoredMovieExactMatch("El dia de la boda (2005)", []tmdbResult{r}); !ok {
		t.Fatal("accent evidence ignored")
	}
	r2 := r
	r2.ID = 2
	if _, ok := monitoredMovieExactMatch("El dia de la boda (2005)", []tmdbResult{r, r2}); ok {
		t.Fatal("ambiguous IDs accepted")
	}
}
