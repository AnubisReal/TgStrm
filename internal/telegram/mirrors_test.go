package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestMessageIDFromUpdatesUsesRandomIDMapping(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateMessageID{ID: 91, RandomID: 400},
		&tg.UpdateMessageID{ID: 92, RandomID: 500},
	}}
	if got := messageIDFromUpdates(updates, 500); got != 92 {
		t.Fatalf("messageIDFromUpdates() = %d, want 92", got)
	}
}

func TestMessageIDFromUpdatesFallsBackToCreatedChannelMessage(t *testing.T) {
	updates := &tg.UpdatesCombined{Updates: []tg.UpdateClass{
		&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 77}},
	}}
	if got := messageIDFromUpdates(updates, 500); got != 77 {
		t.Fatalf("messageIDFromUpdates() = %d, want 77", got)
	}
}
