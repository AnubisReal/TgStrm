package telegram

import (
	"context"
	"errors"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"testing"
	"time"
)

func TestHistoryReadGateSharesFloodWait(t *testing.T) {
	var g historyReadGate
	calls := 0
	call := func() (tg.MessagesMessagesClass, error) { calls++; return nil, tgerr.New(420, "FLOOD_WAIT_30") }
	_, _ = g.read(context.Background(), call)
	_, err := g.read(context.Background(), call)
	if wait, ok := tgerr.AsFloodWait(err); !ok || wait < 29*time.Second || calls != 1 {
		t.Fatalf("wait=%v calls=%d err=%v", wait, calls, err)
	}
	g.resume = time.Now().Add(-time.Second)
	_, _ = g.read(context.Background(), call)
	if calls != 2 {
		t.Fatal("did not resume after deadline")
	}
}

func TestHistoryReadGateCanceledWhileQueued(t *testing.T) {
	var g historyReadGate
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = g.read(context.Background(), func() (tg.MessagesMessagesClass, error) { close(entered); <-release; return nil, nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.read(ctx, func() (tg.MessagesMessagesClass, error) { t.Error("called canceled request"); return nil, nil })
	close(release)
	<-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}
