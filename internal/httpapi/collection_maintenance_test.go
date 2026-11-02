package httpapi

import (
	"errors"
	"fmt"
	"github.com/gotd/td/tgerr"
	"testing"
	"time"
)

func TestCollectionRetryRespectsTelegramWait(t *testing.T) {
	for _, item := range []struct {
		err  error
		wait time.Duration
	}{
		{tgerr.New(420, "FLOOD_WAIT_137"), 137 * time.Second},
		{fmt.Errorf("read topics: %w", tgerr.New(420, "FLOOD_WAIT_600")), 10 * time.Minute},
		{errors.New("temporary connection failure"), 30 * time.Second},
	} {
		if got := collectionRetryDelay(item.err); got != item.wait {
			t.Fatalf("retry %v: got %v want %v", item.err, got, item.wait)
		}
	}
}
