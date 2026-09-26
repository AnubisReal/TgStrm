package telegram

import (
	"context"
	"errors"
	"log"
	"time"

	td "github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Four media connections leave one connection available for a newly requested
// seek while the previous HTTP response can still have three prefetched parts
// in flight. These sessions carry file RPCs only, as recommended by Telegram.
const mediaPoolConnections int64 = 4
const mediaPoolRetryCooldown = 10 * time.Minute

type mediaPool struct {
	owner   *td.Client
	invoker td.CloseInvoker
	api     *tg.Client
}

// mediaAPI returns a warm, reusable file-only pool for the document's media
// datacenter. Pool creation also transfers the existing authorization to that
// DC; no additional login or user-visible session is required.
func (s *Service) mediaAPI(ctx context.Context, dcID int) (*tg.Client, error) {
	if dcID <= 0 {
		return nil, errors.New("invalid media datacenter")
	}
	s.apiMu.RLock()
	client := s.runtimeClient
	primaryAPI := s.runtimeAPI
	s.apiMu.RUnlock()
	if client == nil || primaryAPI == nil {
		return nil, errors.New("telegram is reconnecting")
	}

	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	if cached, ok := s.mediaPools[dcID]; ok && cached.owner == client {
		return cached.api, nil
	}
	if retryAfter := s.mediaPoolRetryAfter[dcID]; time.Now().Before(retryAfter) {
		return primaryAPI, nil
	}
	if s.mediaPools == nil {
		s.mediaPools = make(map[int]mediaPool)
	}
	if s.mediaPoolRetryAfter == nil {
		s.mediaPoolRetryAfter = make(map[int]time.Time)
	}

	// Prefer Telegram's media-only endpoint. Some networks cannot reach the
	// advertised media endpoint even though the regular endpoint for the same
	// file DC works, so fall back to an equally isolated data pool.
	mediaCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	invoker, err := client.MediaOnly(mediaCtx, dcID, mediaPoolConnections)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Printf("Telegram media-only DC %d unavailable, using dedicated data pool: %v", dcID, err)
		invoker, err = client.DC(ctx, dcID, mediaPoolConnections)
		if err != nil {
			// The normal gotd invoker already performs FILE_MIGRATE routing and
			// was the proven path before dedicated pools. Keep streaming available
			// and suppress repeated auth transfers that can trigger FLOOD_WAIT.
			s.mediaPoolRetryAfter[dcID] = time.Now().Add(mediaPoolRetryCooldown)
			log.Printf("Telegram dedicated pool for DC %d unavailable; retry after %s and use compatible routing: %v", dcID, mediaPoolRetryCooldown, err)
			return primaryAPI, nil
		}
	}
	pool := mediaPool{owner: client, invoker: invoker, api: tg.NewClient(invoker)}
	s.mediaPools[dcID] = pool
	delete(s.mediaPoolRetryAfter, dcID)
	return pool.api, nil
}

func (s *Service) closeMediaPools(owner *td.Client) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	for dcID, pool := range s.mediaPools {
		if pool.owner == owner {
			_ = pool.invoker.Close()
			delete(s.mediaPools, dcID)
		}
	}
	clear(s.mediaPoolRetryAfter)
}
