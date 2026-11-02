package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	"tgstrm/migrations"
)

type isolatedScopeDB struct {
	tx     *sql.Tx
	schema string
}

func TestCollectionUpgradeFromExistingSchema(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("requires explicit database audit directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database config unavailable")
	}
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
	schema := fmt.Sprintf("collection_upgrade_%d", time.Now().UnixNano())
	if _, err = tx.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "037_library_collections.sql" {
			continue
		}
		body, readErr := migrations.Files.ReadFile(file.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(body), "tgstrm", schema)); err != nil {
			t.Fatalf("migration %s: %v", file.Name(), err)
		}
	}
	account := "00000000-0000-0000-0000-000000000001"
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".telegram_accounts(id) VALUES($1)", account); err != nil {
		t.Fatal(err)
	}
	var chatID string
	if err = tx.QueryRowContext(ctx, "INSERT INTO "+schema+".chats(account_id,telegram_peer_type,telegram_peer_id,title,is_forum) VALUES($1,'channel',123,'Existing group',true) RETURNING id::text", account).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".settings(key,value) VALUES('active_chat_id',to_jsonb($1::text))", chatID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".library_targets(name,media_type,root_path,public_base_url) VALUES('Existing STRM','shows','/existing/shows','https://example.invalid')"); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("037_library_collections.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(body), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	manifestDDL, err := migrations.Files.ReadFile("038_topic_replication_manifests.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(manifestDDL), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	var topicID string
	if err = tx.QueryRowContext(ctx, "INSERT INTO "+schema+".topics(chat_id,telegram_topic_id,title) VALUES($1,12,'Existing topic') RETURNING id::text", chatID).Scan(&topicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".topic_replication_manifests(account_id,source_chat_id,source_topic_id,status,evidence,video_ids) VALUES($1,$2,$3,'pending','{}','[]')", account, chatID, topicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "SAVEPOINT manifest_constraint"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE "+schema+".topic_replication_manifests SET source_chat_id=99999"); err == nil {
		t.Fatal("manifest accepted an unrelated source group")
	}
	if _, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT manifest_constraint"); err != nil {
		t.Fatal(err)
	}
	scopeDDL, err := migrations.Files.ReadFile("039_catalog_destination_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(scopeDDL), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	jobDDL, err := migrations.Files.ReadFile("040_collection_replication_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(jobDDL), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	recoveryDDL, err := migrations.Files.ReadFile("041_replication_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(recoveryDDL), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	recoveryLinksDDL, err := migrations.Files.ReadFile("043_collection_link_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(recoveryLinksDDL), "tgstrm.", schema+".")); err != nil {
		t.Fatal(err)
	}
	boundaryDDL, err := migrations.Files.ReadFile("042_replication_snapshot_boundary.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(boundaryDDL), "tgstrm", schema)); err != nil {
		t.Fatal(err)
	}
	backoffDDL, err := migrations.Files.ReadFile("044_collection_maintenance_backoff.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(backoffDDL), "tgstrm.", schema+".")); err != nil {
		t.Fatal(err)
	}
	var managedCount int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".managed_catalog_chats").Scan(&managedCount); err != nil || managedCount != 1 {
		t.Fatalf("legacy membership changed: %d %v", managedCount, err)
	}
	scope, err := (Resolver{DB: isolatedScopeDB{tx, schema}}).Active(ctx, account)
	if err != nil || scope.Mode != "group" || scope.GroupID != chatID {
		t.Fatalf("upgrade changed target: %+v %v", scope, err)
	}
	var targets int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".catalog_targets").Scan(&targets); err != nil || targets != 0 {
		t.Fatalf("upgrade enabled target: %d %v", targets, err)
	}
	var root string
	if err = tx.QueryRowContext(ctx, "SELECT root_path FROM "+schema+".library_targets WHERE name='Existing STRM'").Scan(&root); err != nil || root != "/existing/shows" {
		t.Fatalf("STRM destination changed: %q %v", root, err)
	}
}

func (d isolatedScopeDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.tx.QueryRowContext(ctx, strings.ReplaceAll(query, "tgstrm.", d.schema+"."), args...)
}
func (d isolatedScopeDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.tx.QueryContext(ctx, strings.ReplaceAll(query, "tgstrm.", d.schema+"."), args...)
}

// Explicit local audit uses a transaction-isolated schema, never the catalog.
func TestCollectionMigrationIsolated(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("requires explicit database audit directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database config unavailable")
	}
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
	schema := fmt.Sprintf("collection_audit_%d", time.Now().UnixNano())
	if _, err = tx.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	base := `CREATE TABLE SCHEMA.telegram_accounts(id uuid PRIMARY KEY);
	CREATE TABLE SCHEMA.chats(id bigint PRIMARY KEY,account_id uuid NOT NULL REFERENCES SCHEMA.telegram_accounts(id),is_active boolean NOT NULL DEFAULT true,is_forum boolean NOT NULL DEFAULT true);
	CREATE TABLE SCHEMA.settings(key text PRIMARY KEY,value jsonb NOT NULL,revision bigint NOT NULL DEFAULT 1);
 INSERT INTO SCHEMA.telegram_accounts VALUES('00000000-0000-0000-0000-000000000001'),('00000000-0000-0000-0000-000000000002');
 INSERT INTO SCHEMA.chats VALUES(1,'00000000-0000-0000-0000-000000000001'),(2,'00000000-0000-0000-0000-000000000002');`
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(base, "SCHEMA", schema)); err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.Files.ReadFile("037_library_collections.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(raw), "tgstrm.", schema+".")); err != nil {
		t.Fatal(err)
	}
	scopeView, err := migrations.Files.ReadFile("039_catalog_destination_scope.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(scopeView), "tgstrm.", schema+".")); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = tx.QueryRowContext(ctx, "INSERT INTO "+schema+".library_collections(account_id,name) VALUES('00000000-0000-0000-0000-000000000001','Mi biblioteca') RETURNING id::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".collection_groups(collection_id,account_id,destination_chat_id,bucket,media_kind) VALUES($1,'00000000-0000-0000-0000-000000000001',1,'N','series')", id); err != nil {
		t.Fatal(err)
	}
	account := "00000000-0000-0000-0000-000000000001"
	resolver := Resolver{DB: isolatedScopeDB{tx, schema}}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".settings(key,value) VALUES('active_chat_id','\"1\"')"); err != nil {
		t.Fatal(err)
	}
	scope, err := resolver.Active(ctx, account)
	if err != nil || scope.Mode != "group" || scope.GroupID != "1" {
		t.Fatalf("legacy scope: %+v %v", scope, err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE "+schema+".library_collections SET state='ready' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO "+schema+".catalog_targets(account_id,mode,collection_id) VALUES($1,'collection',$2)", account, id); err != nil {
		t.Fatal(err)
	}
	scope, err = resolver.Active(ctx, account)
	if err != nil || scope.CollectionID != id {
		t.Fatalf("collection scope: %+v %v", scope, err)
	}
	groups, err := resolver.DestinationGroups(ctx, account, scope)
	if err != nil || len(groups) != 1 || groups[0] != "1" {
		t.Fatalf("destinations: %v %v", groups, err)
	}
	groups, err = resolver.DestinationGroups(ctx, "00000000-0000-0000-0000-000000000002", scope)
	if err != nil || len(groups) != 0 {
		t.Fatalf("cross-account groups: %v %v", groups, err)
	}
	managed, err := resolver.Contains(ctx, account, "2")
	if err != nil || managed {
		t.Fatalf("cross-account managed: %v %v", managed, err)
	}
	for _, query := range []string{
		"INSERT INTO " + schema + ".collection_groups(collection_id,account_id,destination_chat_id,bucket,media_kind) VALUES($1,'00000000-0000-0000-0000-000000000001',2,'A','series')",
		"INSERT INTO " + schema + ".catalog_targets(account_id,mode) VALUES('00000000-0000-0000-0000-000000000001','collection')",
		"DELETE FROM " + schema + ".library_collections WHERE id=$1",
	} {
		if _, err = tx.ExecContext(ctx, "SAVEPOINT invalid_association"); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(query, "$1") {
			_, err = tx.ExecContext(ctx, query, id)
		} else {
			_, err = tx.ExecContext(ctx, query)
		}
		if err == nil {
			t.Fatal("invalid association accepted", query)
		}
		if _, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT invalid_association"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".chats").Scan(&count); err != nil || count != 2 {
		t.Fatalf("legacy rows changed: %d %v", count, err)
	}
	// Deferred rollback removes all test DDL and data, even on test failure.
}
