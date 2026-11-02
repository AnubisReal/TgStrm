package httpapi

import (
	"database/sql"
	"github.com/gotd/td/tgerr"
	"strings"
	"testing"
	telegramauth "tgstrm/internal/telegram"
	"time"
)

func TestNormalizedDirectMovieCaption(t *testing.T) {
	for _, tc := range []struct {
		title, year, want string
		ok                bool
	}{
		{"  Willow  ", "1988", "Willow (1988)", true},
		{"El viaje de Chihiro", "2001", "El viaje de Chihiro (2001)", true},
		{"Willow", "", "", false}, {"", "1988", "", false}, {"Film", "19xx", "", false},
		{strings.Repeat("á", 1018), "2020", "", false},
	} {
		got, ok := normalizedMovieCaption(tc.title, tc.year)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("%q %q: %q %v", tc.title, tc.year, got, ok)
		}
	}
}
func TestMovieCaptionPermanentErrorsRequireAttention(t *testing.T) {
	for _, err := range []error{sql.ErrNoRows, telegramauth.ErrTopicIdentityConflict, &tgerr.Error{Code: 403, Type: "MESSAGE_AUTHOR_REQUIRED"}, &tgerr.Error{Code: 400, Type: "MESSAGE_EDIT_TIME_EXPIRED"}} {
		if !movieCaptionNeedsAttention(err) {
			t.Fatalf("permanent error retried: %v", err)
		}
	}
	flood := &tgerr.Error{Code: 420, Type: "FLOOD_WAIT", Argument: 14}
	if movieCaptionNeedsAttention(flood) || movieRetryDelay(flood) < 14*time.Second {
		t.Fatal("flood wait must remain pending until Telegram allows retry")
	}
}
