package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	"time"
)

func correctionsAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in database audit")
	}
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database config unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestCorrectionJobsPersistWithinTransaction(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topicID, replicationID string
	if err = tx.QueryRowContext(ctx, `SELECT topic_id::text FROM tgstrm.tmdb_matches WHERE message_id IS NULL AND media_type='tv' LIMIT 1`).Scan(&topicID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.topic_replications LIMIT 1`).Scan(&replicationID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET destination_topic_id=$2::bigint WHERE id::text=$1`, replicationID, topicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.tmdb_matches SET title=title || ' audit',tmdb_id=tmdb_id+1000000 WHERE topic_id::text=$1 AND message_id IS NULL`, topicID); err != nil {
		t.Fatal(err)
	}
	var desired, status string
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT desired_title,status,revision FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1`, topicID).Scan(&desired, &status, &revision); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || !strings.Contains(desired, "audit") || !strings.Contains(desired, "[tmdbid-") {
		t.Fatalf("incorrect persisted identity: %s %s", status, desired)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.tmdb_matches SET title=title || ' latest' WHERE topic_id::text=$1 AND message_id IS NULL`, topicID); err != nil {
		t.Fatal(err)
	}
	var latest int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1`, topicID).Scan(&latest); err != nil || latest != revision+1 {
		t.Fatal("latest correction did not replace earlier work", err)
	}
	var libraryID, assetID, old string
	if err = tx.QueryRowContext(ctx, `SELECT library_id::text,asset_id::text,relative_path FROM tgstrm.stream_exports LIMIT 1`).Scan(&libraryID, &assetID, &old); errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.stream_assets LIMIT 1`).Scan(&assetID); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.library_targets(name,media_type,root_path,public_base_url,is_enabled) VALUES('Transactional audit','shows','/tmp/tgstrm-transaction-audit-' || gen_random_uuid()::text,'http://localhost',false) RETURNING id::text`).Scan(&libraryID); err != nil {
			t.Fatal(err)
		}
		old = "Old/Season 01/S01E01.strm"
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.stream_exports(library_id,asset_id,relative_path,content_hash,status) VALUES($1::uuid,$2::uuid,$3,decode('00','hex'),'written')`, libraryID, assetID, old); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.retired_export_paths(library_id,asset_id,relative_path,content_hash) SELECT library_id,asset_id,relative_path,content_hash FROM tgstrm.stream_exports WHERE library_id::text=$1 AND asset_id::text=$2 ON CONFLICT(library_id,asset_id,relative_path) DO UPDATE SET content_hash=EXCLUDED.content_hash`, libraryID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET relative_path='audit-successor.strm',status='error' WHERE library_id::text=$1 AND asset_id::text=$2`, libraryID, assetID); err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err = tx.QueryRowContext(ctx, `SELECT relative_path FROM tgstrm.retired_export_paths WHERE library_id::text=$1 AND asset_id::text=$2 AND relative_path=$3`, libraryID, assetID, old).Scan(&persisted); err != nil || persisted != old {
		t.Fatal("failed successor lost prior path", err)
	}
}
func TestConfirmRejectsDuplicateSeriesBeforeStarting(t *testing.T) {
	db := correctionsAuditDB(t)
	var id string
	var revision int64
	err := db.QueryRow(`SELECT p.id::text,p.revision FROM tgstrm.collection_replication_plans p WHERE p.status='review' AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND gr.media_kind='series' AND NOT tr.excluded AND tr.status='eligible' GROUP BY gr.id,tr.match_payload->>'id' HAVING count(*)>1) LIMIT 1`).Scan(&id, &revision)
	if err == sql.ErrNoRows {
		t.Skip("no duplicate review fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]int64{"revision": revision})
	r := httptest.NewRequest("POST", "/confirm", strings.NewReader(string(body)))
	r.SetPathValue("planID", id)
	w := httptest.NewRecorder()
	a := &API{db: db}
	a.confirmReplicationPlan(w, r)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "duplicate_series") {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
	}
	var status string
	var after int64
	if err = db.QueryRow(`SELECT status,revision FROM tgstrm.collection_replication_plans WHERE id::text=$1`, id).Scan(&status, &after); err != nil || status != "review" || after != revision {
		t.Fatal("audit started or modified the plan", err)
	}
}
