package httpapi

import (
	"context"
	"testing"
)

func TestExternalMatchedDestinationDoesNotJoinCatalogueDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	a := &API{db: db}
	var target, sourceTopic, sourceGroup string
	err := db.QueryRowContext(ctx, `SELECT c.id::text FROM tgstrm.chats c WHERE c.is_active AND c.is_forum AND NOT EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=c.id) LIMIT 1`).Scan(&target)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRowContext(ctx, `SELECT t.id::text,t.chat_id::text FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE t.is_active AND c.is_active AND t.chat_id::text<>$1 AND EXISTS(SELECT 1 FROM tgstrm.messages m WHERE m.topic_id=t.id AND m.media_type='video' AND m.is_active) LIMIT 1`, target).Scan(&sourceTopic, &sourceGroup)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.resolveManualImportDestination(ctx, sourceGroup, sourceTopic, target, "", "tv", 1399); err != nil || got != target {
		t.Fatalf("explicit external group: %s %v", got, err)
	}
	found := false
	for _, group := range a.importDestinationGroups(ctx, sourceGroup) {
		if group["id"] == target {
			found = true
			if group["catalogue"] != false {
				t.Fatal("external forum marked as default catalogue")
			}
		}
	}
	if !found {
		t.Fatal("external target not offered")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var job string
	if err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.catalog_imports(source_topic_id,destination_title,status,tmdb_media_type,tmdb_id) VALUES($1::bigint,'Audit rollback','pending','tv',1399) RETURNING id::text`, sourceTopic).Scan(&job); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, catalogImportDestinationScopeSQL, job, target, false); err != nil {
		t.Fatal(err)
	}
	var enabled, managed bool
	if err = tx.QueryRowContext(ctx, `SELECT ci.catalogue_enabled,EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=ci.destination_chat_id) FROM tgstrm.catalog_imports ci WHERE ci.id::text=$1`, job).Scan(&enabled, &managed); err != nil {
		t.Fatal(err)
	}
	if enabled || managed {
		t.Fatal("external matched import enrolled the target in the catalogue")
	}
	// A retained replication scope must continue to behave as before.
	if _, err = tx.ExecContext(ctx, catalogImportDestinationScopeSQL, job, target, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT ci.catalogue_enabled,EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=ci.destination_chat_id) FROM tgstrm.catalog_imports ci WHERE ci.id::text=$1`, job).Scan(&enabled, &managed); err != nil {
		t.Fatal(err)
	}
	if !enabled || !managed {
		t.Fatal("legacy replica scope lost")
	}
	// The transaction is rolled back: no topic, copy job or event is sent.
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('catalog.import.progress','topic',$1::bigint,jsonb_build_object('import_id',$2::text,'reason',$3::text))`, sourceTopic, job, "queued"); err != nil {
		t.Fatal(err)
	}
}
