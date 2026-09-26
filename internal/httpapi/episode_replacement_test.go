package httpapi

import (
	"testing"
	"time"
)

func TestShouldReplaceEpisodeOnlyWithNewerPostActivationMessage(t *testing.T) {
	started := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	if !shouldReplaceEpisode(started, started.Add(time.Minute), 102, 101) {
		t.Fatal("expected a newer post-activation message to replace the old episode")
	}
	for _, test := range []struct {
		name                 string
		started, incoming    time.Time
		incomingID, storedID int
	}{
		{"feature disabled", time.Time{}, started.Add(time.Minute), 102, 101},
		{"historical duplicate", started, started.Add(-time.Minute), 102, 101},
		{"older message", started, started.Add(time.Minute), 100, 101},
		{"same message", started, started.Add(time.Minute), 101, 101},
	} {
		if shouldReplaceEpisode(test.started, test.incoming, test.incomingID, test.storedID) {
			t.Fatalf("%s should not replace the current episode", test.name)
		}
	}
}
