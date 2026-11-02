package telegram

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// History readers share Telegram's method quota. A wait received by one worker
// must prevent another reader from immediately repeating that request.
type historyReadGate struct {
	once     sync.Once
	token    chan struct{}
	resume   time.Time
	nextRead time.Time
}

func (g *historyReadGate) read(ctx context.Context, call func() (tg.MessagesMessagesClass, error)) (tg.MessagesMessagesClass, error) {
	g.once.Do(func() { g.token = make(chan struct{}, 1); g.token <- struct{}{} })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.token:
	}
	defer func() { g.token <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if left := time.Until(g.resume); left > 0 {
		return nil, tgerr.New(420, fmt.Sprintf("FLOOD_WAIT_%d", int(math.Ceil(left.Seconds()))))
	}
	if call == nil {
		return nil, nil
	}
	if left := time.Until(g.nextRead); left > 0 {
		timer := time.NewTimer(left)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	result, err := call()
	g.nextRead = time.Now().Add(400 * time.Millisecond)
	if wait, ok := tgerr.AsFloodWait(err); ok && wait > 0 {
		g.resume = time.Now().Add(wait)
	}
	return result, err
}
