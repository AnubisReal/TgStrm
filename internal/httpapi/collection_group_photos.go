package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const collectionGroupPhotoClaimSQL = `WITH candidate AS (
 SELECT cg.id FROM tgstrm.collection_groups cg JOIN tgstrm.chats c ON c.id=cg.destination_chat_id JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id
 WHERE cg.state='ready' AND c.is_active AND lc.state='ready' AND cg.photo_status<>'applied' AND cg.photo_next_attempt_at<=now()
 ORDER BY cg.photo_next_attempt_at,cg.id FOR UPDATE OF cg SKIP LOCKED LIMIT 1
 ) UPDATE tgstrm.collection_groups cg SET photo_next_attempt_at=now()+interval '2 minutes' FROM candidate WHERE cg.id=candidate.id RETURNING cg.id::text,cg.destination_chat_id::text,cg.media_kind,cg.bucket`

// One durable photo job per worker tick. Photo errors never fail video imports.
func (a *API) processCollectionGroupPhoto(ctx context.Context) error {
	var id, groupID, kind, bucket string
	err := a.db.QueryRowContext(ctx, collectionGroupPhotoClaimSQL).Scan(&id, &groupID, &kind, &bucket)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	path, err := a.ensureCabinetArtwork(ctx, kind, bucket)
	var photoID int64
	if err == nil {
		photoID, err = a.telegram.ApplyCollectionGroupPhoto(ctx, groupID, path)
	}
	if err != nil {
		delay := collectionRetryDelay(err)
		if delay < 10*time.Minute {
			delay = 10 * time.Minute
		}
		_, saveErr := a.db.ExecContext(ctx, `UPDATE tgstrm.collection_groups SET photo_status='attention',photo_last_error=$2,photo_next_attempt_at=now()+($3::double precision*interval '1 second') WHERE id::text=$1`, id, err.Error(), delay.Seconds())
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	_, err = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.collection_groups SET photo_status='applied',telegram_photo_id=$2,photo_last_error=NULL WHERE id::text=$1 RETURNING collection_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'library.replication.progress','library',0,jsonb_build_object('collection_id',collection_id::text,'reason','group_photo') FROM changed`, id, photoID)
	return err
}
