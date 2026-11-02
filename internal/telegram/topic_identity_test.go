package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIdentityRejectsOversizedTitleBeforeTelegram(t *testing.T) {
	service := &Service{}
	err := service.CorrectReplicatedTopicIdentity(context.Background(), "1", "old", strings.Repeat("á", 65), "", 1)
	if !errors.Is(err, ErrTopicIdentityConflict) {
		t.Fatalf("expected actionable conflict, got %v", err)
	}
}
