package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestCompletedImportRecoveryUsesDeliveryIdentities(t *testing.T) {
	db := correctionsAuditDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var job, source, topic string
	if err = tx.QueryRow(`SELECT ci.id::text,cim.source_message_id::text,ci.destination_topic_id::text FROM tgstrm.catalog_imports ci JOIN tgstrm.catalog_import_messages cim ON cim.import_id=ci.id JOIN tgstrm.messages m ON m.id=cim.destination_message_id WHERE ci.status='completed' AND ci.destination_topic_id=4092959 AND cim.status='completed' AND cim.destination_deleted_at IS NULL AND m.is_active LIMIT 1`).Scan(&job, &source, &topic); err != nil {
		t.Fatal(err)
	}
	q := strings.Replace(completedImportRecoverySQL, "WHERE ci.status='completed'", "WHERE ci.id::text=$1 AND ci.status='completed'", 1)
	check := func(want bool) {
		t.Helper()
		var group, id string
		err := tx.QueryRow(q, job).Scan(&group, &id)
		if (err == nil) != want || (err != nil && !errors.Is(err, sql.ErrNoRows)) {
			t.Fatalf("recovery expected %v: %v", want, err)
		}
	}
	check(false) // 111 historical deliveries, 110 remaining: the tombstone is respected.
	if _, err = tx.Exec(`UPDATE tgstrm.catalog_imports SET requested_count=requested_count+100 WHERE id::text=$1`, job); err != nil {
		t.Fatal(err)
	}
	check(false) // Historic count never overrides known identities.
	if _, err = tx.Exec(`UPDATE tgstrm.catalog_import_messages SET destination_message_id=NULL,destination_telegram_message_id=2147483000 WHERE import_id::text=$1 AND source_message_id::text=$2`, job, source); err != nil {
		t.Fatal(err)
	}
	check(true) // A real indexing gap must be recovered even with other videos present.
	if _, err = tx.Exec(`UPDATE tgstrm.catalog_import_messages SET destination_deleted_at=now() WHERE import_id::text=$1 AND source_message_id::text=$2`, job, source); err != nil {
		t.Fatal(err)
	}
	check(false) // Explicitly deleted destinations must never be recreated by this worker.
}
