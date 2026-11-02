package httpapi

import (
	"errors"
	"github.com/gotd/td/tgerr"
	"testing"
	"time"
)

func TestMonitoredMovieRequiresCompleteMetadata(t *testing.T) {
	valid := tmdbResult{ID: 123, Title: "Película", ReleaseDate: "2020-01-03", PosterPath: "/poster.jpg"}
	if err := validateMonitoredMovieMatch(valid); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"", "2020", "2020-99-99"} {
		item := valid
		item.ReleaseDate = date
		if validateMonitoredMovieMatch(item) == nil {
			t.Fatalf("accepted date %q", date)
		}
	}
	item := valid
	item.PosterPath = ""
	if validateMonitoredMovieMatch(item) == nil {
		t.Fatal("accepted a missing poster")
	}
	if got := movieCaption(valid); got != "Película (2020)" {
		t.Fatal(got)
	}
}
func TestMovieRetryRespectsTelegramCooldown(t *testing.T) {
	err := &tgerr.Error{Code: 420, Type: "FLOOD_WAIT", Argument: 14}
	if got := movieRetryDelay(err); got < 14*time.Second {
		t.Fatalf("cooldown shortened: %s", got)
	}
	if got := movieRetryDelay(errors.New("offline")); got < time.Second {
		t.Fatal(got)
	}
}

func TestMovieFailureStageKeepsUncertainAcceptanceBlocked(t *testing.T) {
	if got := movieFailureStage("presenting", errors.New("network timeout")); got != "presenting" {
		t.Fatal("uncertain poster acceptance must stay protected")
	}
	if got := movieFailureStage("sending", errors.New("network timeout")); got != "sending" {
		t.Fatal("uncertain video acceptance must stay protected")
	}
}

func TestMovieDefinitePosterFailureDoesNotBlockOtherDeliveries(t *testing.T) {
	err := &tgerr.Error{Code: 400, Type: "WEBPAGE_CURL_FAILED"}
	if movieFailureStage("presenting", err) != "ready" {
		t.Fatal("definite poster failure must allow other deliveries")
	}
	if movieFailureStage("sending", err) != "sending" {
		t.Fatal("video stage must not be reset")
	}
}
