package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gotd/td/tgerr"
	"tgstrm/internal/library"
)

const collectionManifestRecoverySQL = `SELECT c.id::text,t.id::text,mf.id::text FROM tgstrm.topic_replication_manifests mf JOIN tgstrm.topics t ON t.id=mf.source_topic_id JOIN tgstrm.chats c ON c.id=t.chat_id WHERE (mf.invalidated_at IS NOT NULL OR mf.status IN ('pending','scan_failed') OR EXISTS(SELECT 1 FROM tgstrm.topic_replications stale WHERE stale.manifest_id=mf.id AND stale.import_id IS NOT NULL AND stale.status='queued' AND stale.manifest_revision IS DISTINCT FROM mf.revision)) AND mf.next_attempt_at<=now() AND t.is_active AND c.is_active AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.source_topic_id=t.id AND tr.import_id IS NOT NULL) ORDER BY EXISTS(SELECT 1 FROM tgstrm.topic_replications active_tr JOIN tgstrm.group_replications active_gr ON active_gr.id=active_tr.group_replication_id JOIN tgstrm.collection_replication_plans active_p ON active_p.id=active_gr.plan_id WHERE active_tr.manifest_id=mf.id AND active_tr.status='queued' AND active_p.status='running') DESC,mf.invalidated_at NULLS FIRST,mf.id LIMIT 1`

func (a *API) maintainCollections(ctx context.Context) error {
	// Repair drafts invalidated before the discovery worker could revisit them.
	if _, err := a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans p SET status='preparing',revision=revision+1,next_attempt_at=now(),updated_at=now() WHERE p.status='review' AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id WHERE gr.plan_id=p.id AND tr.import_id IS NULL AND NOT tr.excluded AND (mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision))`); err != nil {
		return err
	}
	var sourceGroup, sourceTopic, pendingManifestID string
	err := a.db.QueryRowContext(ctx, collectionManifestRecoverySQL).Scan(&sourceGroup, &sourceTopic, &pendingManifestID)
	if err == nil {
		manifest, manifestID, revision, scanErr := a.telegram.ScanPersistedTopicEligibility(ctx, sourceGroup, sourceTopic)
		if scanErr != nil {
			delay := collectionRetryDelay(scanErr)
			_, saveErr := a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replication_manifests SET next_attempt_at=now()+($2::double precision*interval '1 second') WHERE id::text=$1`, pendingManifestID, delay.Seconds())
			if saveErr != nil {
				return saveErr
			}
			return scanErr
		}
		eligibility := library.TopicEligibility(manifest.Evidence)
		if eligibility == "eligible" {
			_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET manifest_revision=$2 WHERE manifest_id::text=$1 AND status IN ('queued','completed')`, manifestID, revision)
			if err != nil {
				return err
			}
			_, err = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.mirror_links SET status='active',last_error=NULL WHERE source_topic_id::text=$1 AND status='blocked' AND last_error LIKE 'Elegibilidad:%' RETURNING id,destination_topic_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'mirror.progress','topic',destination_topic_id,jsonb_build_object('link_id',id::text,'reason','collection_scope') FROM changed`, sourceTopic)
		} else {
			_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans p SET status='attention',last_error='El origen contiene archivos incompatibles o requiere revisión',revision=p.revision+1,updated_at=now() WHERE p.status='running' AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND tr.manifest_id::text=$1 AND tr.status='queued')`, manifestID)
			if err != nil {
				return err
			}
			_, err = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.mirror_links SET status='blocked',last_error=$2 WHERE source_topic_id::text=$1 AND status='active' RETURNING id,destination_topic_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'mirror.progress','topic',destination_topic_id,jsonb_build_object('link_id',id::text,'reason','collection_scope') FROM changed`, sourceTopic, "Elegibilidad: el origen requiere revisión ("+eligibility+")")
		}
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Soft deletion retains copies and provenance, while cancelling new deliveries.
	_, err = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.mirror_links ml SET status='blocked',last_error=CASE WHEN NOT dt.is_active OR NOT dc.is_active THEN 'Destino eliminado; no se recreará automáticamente' ELSE 'Origen no disponible; se conservan las copias' END FROM tgstrm.topics st,tgstrm.topics dt,tgstrm.chats sc,tgstrm.chats dc WHERE ml.source_topic_id=st.id AND ml.destination_topic_id=dt.id AND sc.id=st.chat_id AND dc.id=dt.chat_id AND ml.status='active' AND (NOT st.is_active OR NOT sc.is_active OR NOT dt.is_active OR NOT dc.is_active) RETURNING ml.id,ml.destination_topic_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'mirror.progress','topic',destination_topic_id,jsonb_build_object('link_id',id::text,'reason','collection_scope') FROM changed`)
	if err != nil {
		return err
	}
	// Distinguish an unavailable source from a missing destination without deleting
	// copied messages. Recovery remains explicit instead of guessing by title.
	_, err = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.group_replications gr SET status=CASE WHEN NOT dc.is_active THEN 'destination_missing' ELSE 'source_missing' END,last_error=CASE WHEN NOT dc.is_active THEN 'Destino no disponible; no se recreará automáticamente' ELSE 'Origen no disponible; se conservan las copias' END,updated_at=now() FROM tgstrm.chats sc,tgstrm.chats dc WHERE sc.id=gr.source_chat_id AND dc.id=gr.destination_chat_id AND gr.status IN ('running','completed') AND (NOT sc.is_active OR NOT dc.is_active) RETURNING gr.plan_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'library.replication.progress','library',0,jsonb_build_object('plan_id',plan_id::text,'reason','unavailable_group') FROM changed`)
	if err != nil {
		return err
	}
	var replicationID, planID string
	err = a.db.QueryRowContext(ctx, `SELECT gr.id::text,gr.plan_id::text,gr.source_chat_id::text FROM tgstrm.group_replications gr JOIN tgstrm.chats sc ON sc.id=gr.source_chat_id JOIN tgstrm.chats dc ON dc.id=gr.destination_chat_id JOIN tgstrm.collection_replication_plans p ON p.id=gr.plan_id WHERE gr.discover_new_topics AND gr.next_discovery_at<=now() AND gr.status IN ('completed','running') AND sc.is_active AND dc.is_active AND p.confirmed_at IS NOT NULL AND p.status NOT IN ('paused','attention') AND (gr.last_discovered_at IS NULL OR gr.last_discovered_at<now()-interval '5 minutes') ORDER BY gr.last_discovered_at NULLS FIRST LIMIT 1`).Scan(&replicationID, &planID, &sourceGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = a.telegram.RefreshTopics(ctx, sourceGroup); err != nil {
		if tgerr.Is(err, "CHANNEL_PRIVATE", "CHAT_ADMIN_REQUIRED") {
			_, _ = a.db.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.group_replications SET status='access_lost',last_error=$2 WHERE id::text=$1 RETURNING plan_id) INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'library.replication.progress','library',0,jsonb_build_object('plan_id',plan_id::text,'reason','access_lost') FROM changed`, replicationID, strings.TrimSpace(err.Error()))
		} else {
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.group_replications SET last_error=$2,next_discovery_at=now()+($3::double precision*interval '1 second') WHERE id::text=$1`, replicationID, strings.TrimSpace(err.Error()), collectionRetryDelay(err).Seconds())
		}
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO tgstrm.topic_replications(group_replication_id,source_topic_id,source_telegram_topic_id,tmdb_media_type) SELECT $1::uuid,t.id,t.telegram_topic_id,CASE WHEN gr.media_kind='movies' THEN 'movie' ELSE 'tv' END FROM tgstrm.topics t JOIN tgstrm.group_replications gr ON gr.id::text=$1 WHERE t.chat_id::text=$2 AND t.is_active AND NOT t.is_hidden AND t.telegram_topic_id<>1 ON CONFLICT(group_replication_id,source_topic_id) DO NOTHING`, replicationID, sourceGroup)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans SET status='preparing',revision=revision+1,next_attempt_at=now(),updated_at=now() WHERE id::text=$1 AND status IN ('completed','running','review')`, planID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.replication.progress','library',0,jsonb_build_object('plan_id',$1::text,'reason','new_topics'))`, planID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.group_replications SET last_discovered_at=now(),next_discovery_at=now(),last_error=NULL WHERE id::text=$1`, replicationID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Rechecks unblock review independently of slow plan, rename and photo work.
func (a *API) runManifestRecheckWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if a.telegram != nil && a.telegram.Connected() {
			workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			_ = a.telegram.ReconcileManifestDeletion(workCtx)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Match Telegram's requested delay exactly; temporary transport failures use a
// modest backoff instead of permanently retiring a valid source.
func collectionRetryDelay(err error) time.Duration {
	if wait, ok := tgerr.AsFloodWait(err); ok && wait > 0 {
		return wait
	}
	return 30 * time.Second
}
