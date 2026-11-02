package telegram

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"tgstrm/internal/config"
	"tgstrm/internal/library"
	"tgstrm/internal/storage/postgres"
	"tgstrm/migrations"
)

func TestManifestRelevantSignature(t *testing.T) {
	m := manifestDocument(1, "episode.mkv", "video/mp4")
	m.Message = "Serie S01E01"
	plain := &tg.Message{ID: 50, Message: "irrelevant text"}
	irrelevant := manifestMessageSignature(plain)
	plain.Message = "another unrelated text"
	if manifestMessageSignature(plain) != irrelevant {
		t.Fatal("unrelated text edit changed eligibility signature")
	}
	original := manifestMessageSignature(m)
	media := m.Media.(*tg.MessageMediaDocument)
	raw, _ := media.GetDocument()
	d, _ := raw.AsNotEmpty()
	d.FileReference = []byte("renewed")
	d.AccessHash = 999
	m.Views = 100
	m.EditDate = 123
	if manifestMessageSignature(m) != original {
		t.Fatal("transport/view metadata caused invalidation")
	}
	m.Message = "Serie S01E02"
	if manifestMessageSignature(m) == original {
		t.Fatal("caption edit was ignored")
	}
	m.Message = "Serie S01E01"
	d.Attributes = []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: "episode.zip"}}
	if manifestMessageSignature(m) == original {
		t.Fatal("media/filename change was ignored")
	}
}

func TestManifestRecheckComparison(t *testing.T) {
	original := TopicManifest{Evidence: library.TopicEvidence{Complete: true, Videos: 2}, VideoIDs: []int{1, 2}, MessageIDs: []int{1, 2, 3}, MessageSignatures: map[string]string{"1": "a", "2": "b", "3": "c"}}
	evidence, _ := marshalManifestEvidence(original)
	videos, _ := json.Marshal(original.VideoIDs)
	same := original
	same.VideoIDs = []int{2, 1}
	if !equivalentManifestEvidence(evidence, videos, same) {
		t.Fatal("pagination order invalidated unchanged evidence")
	}
	changed := original
	changed.Evidence.UnclassifiedDocuments = 1
	if equivalentManifestEvidence(evidence, videos, changed) {
		t.Fatal("unknown document ignored")
	}
	changed = original
	changed.MessageSignatures = map[string]string{"1": "caption edit", "2": "b", "3": "c"}
	if equivalentManifestEvidence(evidence, videos, changed) {
		t.Fatal("caption change ignored")
	}
	legacy, _ := json.Marshal(original.Evidence)
	if !equivalentManifestEvidence(legacy, videos, same) {
		t.Fatal("legacy compatible evidence discarded")
	}
}

// The additive migration and event writes are exercised in one rollback-only
// transaction; neither real Telegram nor the production queue is modified.
func TestManifestIncrementalDatabaseRegression(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in rollback audit")
	}
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database unavailable")
	}
	ctx := context.Background()
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var installed bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('tgstrm.manifest_rechecks') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		migration, err := migrations.Files.ReadFile("060_incremental_manifest_checks.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var group string
	var channel int64
	if err = tx.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id FROM tgstrm.chats c JOIN tgstrm.topic_replication_manifests mf ON mf.source_chat_id=c.id WHERE c.account_id=$1 AND c.is_active GROUP BY c.id HAVING count(*)>1 LIMIT 1`, accountID).Scan(&group, &channel); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT mf.id::text,t.id::text,t.telegram_topic_id FROM tgstrm.topic_replication_manifests mf JOIN tgstrm.topics t ON t.id=mf.source_topic_id WHERE mf.source_chat_id::text=$1 AND t.is_active ORDER BY mf.id LIMIT 2`, group)
	if err != nil {
		t.Fatal(err)
	}
	type scope struct {
		id, topic string
		remote    int
	}
	var scopes []scope
	for rows.Next() {
		var v scope
		if err = rows.Scan(&v.id, &v.topic, &v.remote); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, v)
	}
	rows.Close()
	if len(scopes) != 2 {
		t.Fatal("two fixture topics required")
	}
	for i, v := range scopes {
		ids, _ := json.Marshal([]int{2147483001 + i})
		evidence := `{"complete":true,"videos":1,"message_ids":` + string(ids) + `}`
		if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.topic_replication_manifests SET status='eligible',evidence=$2::jsonb,video_ids=$3::jsonb,invalidated_at=NULL,upper_message_id=2147483010 WHERE id::text=$1`, v.id, evidence, string(ids)); err != nil {
			t.Fatal(err)
		}
	}
	var changed bool
	if err = tx.QueryRowContext(ctx, manifestMessageUpdateSQL, accountID, channel, scopes[0].remote, "2147483200", 2147483200, "caption-v1").Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first relevant edit was ignored")
	}
	if err = tx.QueryRowContext(ctx, manifestMessageUpdateSQL, accountID, channel, scopes[0].remote, "2147483200", 2147483200, "caption-v1").Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("duplicate edit invalidated")
	}
	if err = tx.QueryRowContext(ctx, manifestMessageUpdateSQL, accountID, channel, scopes[0].remote, "2147483200", 2147483200, "caption-v2").Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed caption ignored")
	}
	// The production invalidation lock must serialize with confirmation's
	// plan-row lock; the competing transaction times out and is rolled back.
	if err = lockManifestPlans(ctx, tx, channel, scopes[0].remote); err != nil {
		t.Fatal(err)
	}
	var planID string
	lockErr := tx.QueryRowContext(ctx, `SELECT p.id::text FROM tgstrm.collection_replication_plans p JOIN tgstrm.group_replications gr ON gr.plan_id=p.id JOIN tgstrm.topic_replications tr ON tr.group_replication_id=gr.id WHERE tr.source_topic_id::text=$1 LIMIT 1`, scopes[0].topic).Scan(&planID)
	if lockErr == nil {
		competing, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		var locked string
		blocked := competing.QueryRowContext(deadline, `SELECT id::text FROM tgstrm.collection_replication_plans WHERE id::text=$1 FOR UPDATE`, planID).Scan(&locked)
		cancel()
		competing.Rollback()
		if blocked == nil {
			t.Fatal("confirmation bypassed invalidation lock")
		}
	}
	var before, otherBefore int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, scopes[0].id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, scopes[1].id).Scan(&otherBefore); err != nil {
		t.Fatal(err)
	}
	if err = invalidateDeletedMessageManifestsTx(ctx, tx, channel, []int{2147483001, 2147483001}); err != nil {
		t.Fatal(err)
	}
	var after, otherAfter int64
	tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, scopes[0].id).Scan(&after)
	tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, scopes[1].id).Scan(&otherAfter)
	if after != before+1 || otherAfter != otherBefore {
		t.Fatalf("scope revisions: %d/%d other %d/%d", before, after, otherBefore, otherAfter)
	}
	if err = invalidateDeletedMessageManifestsTx(ctx, tx, channel, []int{2147483001}); err != nil {
		t.Fatal(err)
	}
	tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, scopes[0].id).Scan(&before)
	if before != after {
		t.Fatal("repeated deletion invalidated again")
	}
	var id string
	var revision int64
	if err = tx.QueryRowContext(ctx, beginManifestScanSQL, accountID, group, scopes[0].topic).Scan(&id, &revision); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	tx.QueryRowContext(ctx, `SELECT evidence->'message_ids' @> '[2147483001]'::jsonb AND video_ids @> '[2147483001]'::jsonb FROM tgstrm.topic_replication_manifests WHERE id::text=$1`, id).Scan(&preserved)
	if !preserved {
		t.Fatal("begin scan destroyed prior inventory")
	}
	if err = invalidateDeletedMessageManifestsTx(ctx, tx, channel, []int{2147483100}); err != nil {
		t.Fatal(err)
	}
	var checkBefore, checkAfter int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1`, id).Scan(&checkBefore); err != nil {
		t.Fatal(err)
	}
	if err = invalidateDeletedMessageManifestsTx(ctx, tx, channel, []int{2147483100}); err != nil {
		t.Fatal(err)
	}
	tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1`, id).Scan(&checkAfter)
	if checkBefore != checkAfter {
		t.Fatal("unknown duplicate requeued")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.manifest_rechecks SET next_attempt_at=now() WHERE manifest_id::text=$1`, id); err != nil {
		t.Fatal(err)
	}
	var claimID, claimGroup, claimTopic string
	var claimChannel int64
	var claimThread int
	var claimGeneration, claimRevision int64
	var claimEvidence, claimVideos []byte
	if err = tx.QueryRowContext(ctx, claimManifestRecheckSQL).Scan(&claimID, &claimGroup, &claimTopic, &claimChannel, &claimThread, &claimGeneration, &claimRevision, &claimEvidence, &claimVideos); err != nil {
		t.Fatal(err)
	}
	var claimed bool
	if err = tx.QueryRowContext(ctx, `SELECT next_attempt_at>now() FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1`, claimID).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("recheck not claimed for crash-safe retry")
	}

}

func TestRecentReviewManifestDatabase(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in rollback audit")
	}
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database unavailable")
	}
	db, err := postgres.Connect(context.Background(), *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id, group, topic string
	if err = tx.QueryRow(`SELECT mf.id::text,c.id::text,t.id::text FROM tgstrm.topic_replication_manifests mf JOIN tgstrm.topics t ON t.id=mf.source_topic_id JOIN tgstrm.chats c ON c.id=t.chat_id WHERE t.is_active AND c.is_active AND c.account_id=$1 LIMIT 1`, accountID).Scan(&id, &group, &topic); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, change string
		want         bool
	}{
		{"fresh", "", true},
		{"changed", `UPDATE tgstrm.topic_replication_manifests SET invalidated_at=now() WHERE id::text=$1`, false},
		{"old", `UPDATE tgstrm.topic_replication_manifests SET scanned_at=now()-interval '10 minutes' WHERE id::text=$1`, false},
		{"incomplete", `UPDATE tgstrm.topic_replication_manifests SET evidence='{"complete":false}'::jsonb WHERE id::text=$1`, false},
		{"deletion_pending", `INSERT INTO tgstrm.manifest_rechecks(manifest_id,next_attempt_at) VALUES($1::uuid,now()) ON CONFLICT(manifest_id) DO NOTHING`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err = tx.Exec(`DELETE FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1`, id); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`UPDATE tgstrm.topic_replication_manifests SET invalidated_at=NULL,status='eligible',scanned_at=now(),evidence='{"complete":true}'::jsonb WHERE id::text=$1`, id); err != nil {
				t.Fatal(err)
			}
			if tc.change != "" {
				if _, err = tx.Exec(tc.change, id); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := tx.Query(recentReviewManifestSQL, group, topic, accountID)
			if err != nil {
				t.Fatal(err)
			}
			got := rows.Next()
			rows.Close()
			if got != tc.want {
				t.Fatalf("reuse %v want %v", got, tc.want)
			}
		})
	}
}
