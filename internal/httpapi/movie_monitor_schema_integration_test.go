package httpapi

import (
	"context"
	"testing"
	"tgstrm/migrations"
)

func TestMovieMonitorSchemaPreventsPrematureDelivery(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var installed bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('tgstrm.movie_monitors') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		body, readErr := migrations.Files.ReadFile("053_movie_monitors.sql")
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	var monitor string
	err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.movie_monitors(source_chat_id,destination_chat_id)
 SELECT a.id,b.id FROM tgstrm.chats a JOIN tgstrm.chats b ON b.id<>a.id
 WHERE NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitors mm WHERE mm.source_chat_id=a.id AND mm.destination_chat_id=b.id AND mm.source_topic_id IS NULL AND mm.destination_topic_id IS NULL)
 LIMIT 1 RETURNING id::text`).Scan(&monitor)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"ready", "presenting", "sending", "completed"} {
		if _, err = tx.ExecContext(ctx, `SAVEPOINT invalid_delivery`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.movie_monitor_deliveries(monitor_id,source_message_id,source_signature,status,presentation_random_id,video_random_id)
  SELECT $1::uuid,id,'test',$2,10001,10002 FROM tgstrm.messages LIMIT 1`, monitor, status)
		if err == nil {
			t.Fatalf("allowed %s without verified identity/presentation", status)
		}
		if _, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT invalid_delivery`); err != nil {
			t.Fatal(err)
		}
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tgstrm.movie_monitors WHERE id=$1::uuid`, monitor).Scan(&state); err != nil || state != "paused" {
		t.Fatalf("unsafe default %s: %v", state, err)
	}
}
