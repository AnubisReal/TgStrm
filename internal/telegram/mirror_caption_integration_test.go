package telegram

import (
	"context"
	"os"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
)

// Exercise the actual SQL used by realtime and recovery. All fixture changes
// and trigger side effects are rolled back, including when the test fails.
func TestMirrorCaptionReplayAndOfflineRecovery(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in PostgreSQL regression")
	}
	vault, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vault.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database config unavailable")
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
	// Compile the production delivery lookup and verify it addresses the
	// exact source message, rather than an undefined alias or an entire topic.
	rows, err := tx.QueryContext(ctx, mirrorDeliverySourceSQL, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var sourceID int64
	var channelID int64
	var topicID, messageID int
	err = tx.QueryRowContext(ctx, `SELECT source.id,c.telegram_peer_id,t.telegram_topic_id,source.telegram_message_id
 FROM tgstrm.mirror_deliveries md JOIN tgstrm.mirror_links ml ON ml.id=md.link_id
 JOIN tgstrm.messages source ON source.id=md.source_message_id JOIN tgstrm.topics t ON t.id=source.topic_id
 JOIN tgstrm.chats c ON c.id=t.chat_id WHERE ml.status='active' AND md.status='completed'
 AND md.destination_message_id IS NOT NULL LIMIT 1`).Scan(&sourceID, &channelID, &topicID, &messageID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.messages SET body='' WHERE id=$1`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries SET source_caption_snapshot='',last_applied_caption='Serie (2020) S01E01 Episodio',desired_caption=NULL,edit_status='idle' WHERE source_message_id=$1`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, reconcileMirrorCaptionEditSQL); err != nil {
		t.Fatal(err)
	}
	var initialStatus string
	if err = tx.QueryRowContext(ctx, `SELECT edit_status FROM tgstrm.mirror_deliveries WHERE source_message_id=$1 LIMIT 1`, sourceID).Scan(&initialStatus); err != nil {
		t.Fatal(err)
	}
	if initialStatus != "idle" {
		t.Fatal("normalized destination was mistaken for a source edit")
	}
	for _, q := range []struct {
		caption string
		changed bool
	}{{"", false}, {"audit-edited", true}, {"audit-edited", false}} {
		result, err := tx.ExecContext(ctx, queueMirrorCaptionEditSQL, channelID, topicID, messageID, q.caption)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := result.RowsAffected()
		if (n > 0) != q.changed {
			t.Fatalf("caption %q: affected %d", q.caption, n)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.messages SET body='audit-offline-edit' WHERE id=$1`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries SET edit_status='idle' WHERE source_message_id=$1`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.ExecContext(ctx, reconcileMirrorCaptionEditSQL)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := result.RowsAffected()
	if n < 1 {
		t.Fatal("offline source edit not recovered")
	}
	var desired, snapshot string
	err = tx.QueryRowContext(ctx, `SELECT desired_caption,source_caption_snapshot FROM tgstrm.mirror_deliveries WHERE source_message_id=$1 AND edit_status='pending' LIMIT 1`, sourceID).Scan(&desired, &snapshot)
	if err != nil || desired != "audit-offline-edit" || snapshot != desired {
		t.Fatalf("offline recovery: %q %q %v", desired, snapshot, err)
	}
}
