package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gotd/td/tgerr"
	"tgstrm/internal/telegram"
	"time"
)

func (a *API) correctTopicIdentities(ctx context.Context) {
	if a.telegram == nil {
		return
	}
	var id, previous, desired, poster string
	var revision, tmdbID, identityRevision, captionRevision int64
	err := a.db.QueryRowContext(ctx, `SELECT topic_id::text,revision,previous_title,desired_title,poster_path,tmdb_id,identity_applied_revision,captions_prepared_revision FROM tgstrm.topic_identity_jobs WHERE status='pending' AND next_attempt_at<=now() ORDER BY updated_at LIMIT 1`).Scan(&id, &revision, &previous, &desired, &poster, &tmdbID, &identityRevision, &captionRevision)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	var locked bool
	if conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended('topic-identity:' || $1,0))`, id).Scan(&locked) != nil || !locked {
		return
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('topic-identity:' || $1,0))`, id)
	var current bool
	if conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1 AND revision=$2 AND status='pending')`, id, revision).Scan(&current) != nil || !current {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	done, stepErr := a.advanceTopicCorrection(callCtx, id, previous, desired, poster, revision, tmdbID, identityRevision, captionRevision)
	err = stepErr
	cancel()
	status := "pending"
	if done {
		status = "completed"
	}
	detail := ""
	delay := 2 * time.Second
	if err != nil {
		status = "pending"
		detail = err.Error()
		delay = 30 * time.Second
		if wait, ok := tgerr.AsFloodWait(err); ok {
			delay = wait
		} else if errors.Is(err, telegram.ErrTopicIdentityConflict) || errors.Is(err, sql.ErrNoRows) {
			status = "attention"
		} else if errors.Is(err, context.Canceled) {
			return
		}
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_identity_jobs SET status=$3,last_error=NULLIF($4,''),next_attempt_at=now()+($5::double precision*interval '1 second'),updated_at=now() WHERE topic_id::text=$1 AND revision=$2`, id, revision, status, detail, delay.Seconds())
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.identity.progress','topic',$1::bigint,jsonb_build_object('status',$2::text))`, id, status)
}

func (a *API) runTopicIdentityWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		a.correctTopicIdentities(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
