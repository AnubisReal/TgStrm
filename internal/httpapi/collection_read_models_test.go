package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	"time"
)

// Opt-in integration audit. These handlers perform only reads against the
// configured local installation. Simulated fixture deletions are rolled back;
// no Telegram call or migration runs here.
func TestCollectionReadModels(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("requires explicit database audit directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database configuration unavailable")
	}
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api := &API{store: store, db: db}
	for _, item := range []struct {
		name    string
		handler func(*httptest.ResponseRecorder)
	}{
		{"collections", func(w *httptest.ResponseRecorder) {
			api.libraryCollections(w, httptest.NewRequest("GET", "/api/v1/library/collections", nil).WithContext(ctx))
		}},
		{"target", func(w *httptest.ResponseRecorder) {
			api.libraryTarget(w, httptest.NewRequest("GET", "/api/v1/library/target", nil).WithContext(ctx))
		}},
		{"jobs", func(w *httptest.ResponseRecorder) {
			api.jobs(w, httptest.NewRequest("GET", "/api/v1/jobs", nil).WithContext(ctx))
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			item.handler(w)
			if w.Code != 200 {
				t.Fatalf("read model failed (%d): %s", w.Code, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("invalid JSON")
			}
		})
	}
	// Exercise persisted plans and memberships as well as empty collection lists.
	// This catches SQL/read-model failures hidden by the first-install state.
	for _, scope := range []struct {
		query, path, key string
		handler          func(http.ResponseWriter, *http.Request)
	}{
		{`SELECT id::text FROM tgstrm.collection_replication_plans ORDER BY created_at LIMIT 20`, "/api/v1/library/replication-plans/", "planID", api.replicationPlan},
		{`SELECT id::text FROM tgstrm.library_collections WHERE state<>'archived' ORDER BY created_at LIMIT 20`, "/api/v1/library/collections/", "collectionID", api.libraryCollection},
	} {
		rows, err := db.QueryContext(ctx, scope.query)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		for _, id := range ids {
			r := httptest.NewRequest("GET", scope.path+id, nil).WithContext(ctx)
			r.SetPathValue(scope.key, id)
			w := httptest.NewRecorder()
			scope.handler(w, r)
			if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("persisted %s read failed (%d): %s", scope.key, w.Code, w.Body.String())
			}
			if scope.key == "planID" {
				var data struct {
					Status string `json:"status"`
				}
				if err = json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Fatal(err)
				}
				if data.Status == "completed" {
					// A deliberately stale revision cannot start or approve a changed
					// draft. A terminal plan must return its result without resending.
					r := httptest.NewRequest("POST", scope.path+id+"/confirm", strings.NewReader(`{"revision":-1}`)).WithContext(ctx)
					r.SetPathValue("planID", id)
					result := httptest.NewRecorder()
					api.confirmReplicationPlan(result, r)
					if result.Code != 202 && result.Code != 409 {
						t.Fatalf("terminal confirmation: %d", result.Code)
					}
				}
			}
		}
	}
	// Saved library links resolve by topic identity even when the selected
	// primary destination is another group. No Telegram request is needed.
	rows, err := db.QueryContext(ctx, `SELECT t.id::text,t.chat_id::text FROM tgstrm.topics t JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id WHERE t.is_active AND c.is_active ORDER BY t.id LIMIT 30`)
	if err != nil {
		t.Fatal(err)
	}
	type location struct{ topic, group string }
	var locations []location
	for rows.Next() {
		var item location
		if err = rows.Scan(&item.topic, &item.group); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		locations = append(locations, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	for _, item := range locations {
		r := httptest.NewRequest("GET", "/api/v1/library/topics/"+item.topic+"/location", nil).WithContext(ctx)
		r.SetPathValue("topicID", item.topic)
		w := httptest.NewRecorder()
		api.libraryTopicLocation(w, r)
		var response struct {
			GroupID string `json:"group_id"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.GroupID != item.group {
			t.Fatalf("saved topic location incorrect: %d %s", w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/library/topics/missing/location", nil).WithContext(ctx)
	r.SetPathValue("topicID", "missing")
	w := httptest.NewRecorder()
	api.libraryTopicLocation(w, r)
	if w.Code != 404 {
		t.Fatalf("missing topic status: %d", w.Code)
	}

	// EXPLAIN validates the atomic state/event statements against the actual
	// upgraded schema without executing updates or publishing events.
	file, err := parser.ParseFile(token.NewFileSet(), "collection_maintenance.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	parameter := regexp.MustCompile(`\$([0-9]+)`)
	checked := 0
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		query, err := strconv.Unquote(literal.Value)
		if err != nil || !strings.HasPrefix(query, "WITH changed AS") {
			return true
		}
		count := 0
		for _, match := range parameter.FindAllStringSubmatch(query, -1) {
			index, _ := strconv.Atoi(match[1])
			if index > count {
				count = index
			}
		}
		args := make([]any, count)
		for index := range args {
			args[index] = "-1"
		}
		if _, err = db.ExecContext(ctx, "EXPLAIN "+query, args...); err != nil {
			t.Errorf("atomic transition SQL: %v", err)
		}
		checked++
		return true
	})
	if checked != 5 {
		t.Fatalf("atomic transitions audited: %d", checked)
	}

	// Controlled fixture only: validate tombstones within a rolled-back
	// transaction. Other sessions cannot see the simulated deletions.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var source, destination, link, importID string
	err = tx.QueryRowContext(ctx, `SELECT md.source_message_id::text,md.destination_message_id::text,md.link_id::text,tr.import_id::text FROM tgstrm.mirror_deliveries md JOIN tgstrm.topic_replications tr ON tr.source_topic_id=(SELECT source_topic_id FROM tgstrm.mirror_links WHERE id=md.link_id) JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id::text='14b5ae20-084a-4f63-b50f-ba08f9b361b9' AND md.destination_deleted_at IS NULL AND md.destination_message_id IS NOT NULL LIMIT 1`).Scan(&source, &destination, &link, &importID)
	if err == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.messages SET is_active=false WHERE id::text=$1`, source); err != nil {
			t.Fatal(err)
		}
		var removed bool
		if err = tx.QueryRowContext(ctx, `SELECT destination_deleted_at IS NOT NULL FROM tgstrm.mirror_deliveries WHERE link_id::text=$1 AND source_message_id::text=$2`, link, source).Scan(&removed); err != nil || removed {
			t.Fatal("origin deletion affected destination tombstone", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.messages SET is_active=false WHERE id::text=$1`, destination); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRowContext(ctx, `SELECT destination_deleted_at IS NOT NULL FROM tgstrm.mirror_deliveries WHERE link_id::text=$1 AND source_message_id::text=$2`, link, source).Scan(&removed); err != nil || !removed {
			t.Fatal("destination mirror tombstone missing", err)
		}
		if err = tx.QueryRowContext(ctx, `SELECT destination_deleted_at IS NOT NULL FROM tgstrm.catalog_import_messages WHERE import_id::text=$1 AND source_message_id::text=$2`, importID, source).Scan(&removed); err != nil || !removed {
			t.Fatal("destination import tombstone missing", err)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}

}
