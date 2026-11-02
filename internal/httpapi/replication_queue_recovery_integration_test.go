package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReplicationQueueRecoveryDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var child, plan, manifest, job string
	err = tx.QueryRowContext(ctx, `SELECT tr.id::text,gr.plan_id::text,tr.manifest_id::text,tr.import_id::text FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.chats c ON c.id=gr.source_chat_id JOIN tgstrm.topics t ON t.id=tr.source_topic_id WHERE tr.import_id IS NOT NULL AND tr.manifest_id IS NOT NULL AND c.is_active AND t.is_active LIMIT 1`).Scan(&child, &plan, &manifest, &job)
	if err != nil {
		t.Fatal(err)
	}
	claim := strings.Replace(catalogImportClaimSQL, "ORDER BY queued_at,id", "AND id::text=$1 ORDER BY queued_at,id", 1)
	recovery := strings.Replace(collectionManifestRecoverySQL, " ORDER BY EXISTS", " AND mf.id::text=$1 ORDER BY EXISTS", 1)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() {
		exec(`UPDATE tgstrm.collection_replication_plans SET status='running' WHERE id::text=$1`, plan)
		exec(`UPDATE tgstrm.topic_replication_manifests SET status='eligible',invalidated_at=NULL,next_attempt_at=now() WHERE id::text=$1`, manifest)
		exec(`UPDATE tgstrm.topic_replications SET status='queued',manifest_id=$2::uuid,manifest_revision=(SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$2::text) WHERE id::text=$1`, child, manifest)
		exec(`UPDATE tgstrm.catalog_imports SET status='pending',next_attempt_at=now(),lease_token=NULL,lease_expires_at=NULL WHERE id::text=$1`, job)
	}
	for _, tc := range []struct {
		name, change   string
		args           []any
		claim, recover bool
	}{
		{"valid", "", nil, true, false},
		{"obsolete_revision", `UPDATE tgstrm.topic_replication_manifests SET revision=revision+1 WHERE id::text=$1`, []any{manifest}, false, true},
		{"invalidated", `UPDATE tgstrm.topic_replication_manifests SET invalidated_at=now() WHERE id::text=$1`, []any{manifest}, false, true},
		{"pending", `UPDATE tgstrm.topic_replication_manifests SET status='pending' WHERE id::text=$1`, []any{manifest}, false, true},
		{"failed_scan", `UPDATE tgstrm.topic_replication_manifests SET status='scan_failed' WHERE id::text=$1`, []any{manifest}, false, true},
		{"paused", `UPDATE tgstrm.collection_replication_plans SET status='paused' WHERE id::text=$1`, []any{plan}, false, false},
		{"missing_manifest", `UPDATE tgstrm.topic_replications SET manifest_id=NULL WHERE id::text=$1`, []any{child}, false, false},
		{"future_retry", `UPDATE tgstrm.catalog_imports SET next_attempt_at=now()+interval '1 hour' WHERE id::text=$1`, []any{job}, false, false},
		{"live_lease", `UPDATE tgstrm.catalog_imports SET status='running',lease_expires_at=now()+interval '1 hour' WHERE id::text=$1`, []any{job}, false, false},
		{"expired_lease", `UPDATE tgstrm.catalog_imports SET status='running',lease_expires_at=now()-interval '1 hour' WHERE id::text=$1`, []any{job}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			if tc.change != "" {
				exec(tc.change, tc.args...)
			}
			var group, topic, id string
			err := tx.QueryRowContext(ctx, recovery, manifest).Scan(&group, &topic, &id)
			if (err == nil) != tc.recover || (err != nil && !errors.Is(err, sql.ErrNoRows)) {
				t.Fatalf("recovery: %v", err)
			}
			var claimed, token string
			err = tx.QueryRowContext(ctx, claim, job).Scan(&claimed, &token)
			if (err == nil) != tc.claim || (err != nil && !errors.Is(err, sql.ErrNoRows)) {
				t.Fatalf("claim: %v", err)
			}
		})
	}
	t.Run("obsolete_oldest_does_not_starve_valid", func(t *testing.T) {
		reset()
		var otherChild, otherJob, otherManifest string
		if err := tx.QueryRowContext(ctx, `SELECT tr.id::text,tr.import_id::text,tr.manifest_id::text FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id::text=$1 AND tr.id::text<>$2 AND tr.import_id IS NOT NULL AND tr.manifest_id IS NOT NULL AND tr.manifest_id::text<>$3 LIMIT 1`, plan, child, manifest).Scan(&otherChild, &otherJob, &otherManifest); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE tgstrm.topic_replication_manifests SET status='eligible',invalidated_at=NULL WHERE id::text=$1`, otherManifest)
		exec(`UPDATE tgstrm.topic_replications SET status='queued',manifest_revision=(SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$2) WHERE id::text=$1`, otherChild, otherManifest)
		exec(`UPDATE tgstrm.catalog_imports SET status='pending',next_attempt_at=now(),queued_at=now(),lease_token=NULL,lease_expires_at=NULL WHERE id::text=$1`, otherJob)
		exec(`UPDATE tgstrm.catalog_imports SET queued_at=now()-interval '1 hour' WHERE id::text=$1`, job)
		exec(`UPDATE tgstrm.topic_replication_manifests SET revision=revision+1 WHERE id::text=$1`, manifest)
		narrowed := strings.Replace(catalogImportClaimSQL, "ORDER BY queued_at,id", "AND id::text IN ($1,$2) ORDER BY queued_at,id", 1)
		var claimed, token string
		if err := tx.QueryRowContext(ctx, narrowed, job, otherJob).Scan(&claimed, &token); err != nil {
			t.Fatal(err)
		}
		if claimed != otherJob {
			t.Fatal("stale oldest job monopolized queue")
		}
	})

}

func TestCatalogCaptionSourcesReleaseSingleConnection(t *testing.T) {
	db := correctionsAuditDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var topic string
	if err := db.QueryRowContext(ctx, `SELECT m.topic_id::text FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE m.is_active AND m.media_type='video' LIMIT 1`).Scan(&topic); err != nil {
		t.Fatal(err)
	}
	a := &API{db: db}
	sources, err := a.loadCatalogCaptionSources(ctx, topic, nil)
	if err != nil || len(sources) == 0 {
		t.Fatalf("load: %v", err)
	}
	var one int
	if err = db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("result retained sole connection: %v", err)
	}
	subset, err := a.loadCatalogCaptionSources(ctx, topic, []string{sources[0].messageID})
	if err != nil || len(subset) != 1 {
		t.Fatalf("selection changed: %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err = a.loadCatalogCaptionSources(canceled, topic, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
