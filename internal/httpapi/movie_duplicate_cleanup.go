package httpapi

import (
	"context"
	"errors"
	telegramauth "tgstrm/internal/telegram"
	"time"
)

func (a *API) runMovieDuplicateCleanup(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			step, cancel := context.WithTimeout(ctx, 40*time.Second)
			a.cleanMovieDuplicate(step)
			cancel()
		}
	}
}
func (a *API) cleanMovieDuplicate(ctx context.Context) {
	service := a.telegramService()
	if service == nil {
		return
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	var locked bool
	if conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('tgstrm-movie-delivery-worker'))`).Scan(&locked) != nil || !locked {
		return
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT pg_advisory_unlock(hashtext('tgstrm-movie-delivery-worker'))`)
	}()
	var id, monitor string
	if conn.QueryRowContext(ctx, `SELECT j.delivery_id::text,d.monitor_id::text FROM tgstrm.movie_duplicate_cleanup j JOIN tgstrm.movie_monitor_deliveries d ON d.id=j.delivery_id WHERE j.status='pending' AND j.next_attempt_at<=now() ORDER BY j.video_id LIMIT 1`).Scan(&id, &monitor) != nil {
		return
	}
	err = service.DeleteDuplicateMoviePair(ctx, id)
	if err != nil {
		status := "pending"
		if errors.Is(err, telegramauth.ErrTopicIdentityConflict) {
			status = "attention"
		}
		// Flood limits apply to the entire cleanup queue, not just one file.
		_, _ = conn.ExecContext(ctx, `UPDATE tgstrm.movie_duplicate_cleanup SET next_attempt_at=now()+$1::interval WHERE status='pending'`, movieRetryDelay(err).String())
		_, _ = conn.ExecContext(ctx, `UPDATE tgstrm.movie_duplicate_cleanup SET status=$2,last_error=$3,updated_at=now() WHERE delivery_id::text=$1`, id, status, err.Error())
		return
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_monitor_deliveries SET status='excluded',last_error='Archivo idéntico ya presente o reservado en este destino.',lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id::text=$1`, id); err != nil {
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_duplicate_cleanup SET status='completed',last_error=NULL,updated_at=now() WHERE delivery_id::text=$1`, id); err != nil {
		return
	}
	if tx.Commit() == nil {
		a.publishMovieMonitor(ctx, monitor)
	}
}
