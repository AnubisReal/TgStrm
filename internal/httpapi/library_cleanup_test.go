package httpapi

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	"time"
)

func TestCleanupRejectsTraversal(t *testing.T) {
	for _, path := range []string{"", ".", "../outside", "/outside", "Series/../../outside", ".tgstrm-secret/file"} {
		if _, err := cleanupTreeName(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	for path, want := range map[string]string{"Arrow (2012)/Season 01/Episode.strm": "Arrow (2012)", "movie.strm": "movie.strm"} {
		got, err := cleanupTreeName(path)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", path, got, err)
		}
	}
}
func TestCleanupDeletesOwnedTreePreservesRootAndNeighbours(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "Arrow (2012)")
	if err := os.MkdirAll(filepath.Join(tree, "Season 01"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(tree, "poster.jpg"), filepath.Join(tree, "Season 01", "one.strm"), filepath.Join(root, "keep.strm")} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	job := cleanupTree{Root: root, Name: "Arrow (2012)"}
	if err := deleteCleanupTree(job); err != nil {
		t.Fatal(err)
	}
	if err := deleteCleanupTree(job); err != nil {
		t.Fatal("not idempotent", err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatal("tree remains")
	}
	if _, err := os.Stat(filepath.Join(root, "keep.strm")); err != nil {
		t.Fatal("neighbour changed", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("library root deleted")
	}
}
func TestCleanupDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(outside, "keep.strm")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Series A")); err != nil {
		t.Skip(err)
	}
	if err := deleteCleanupTree(cleanupTree{Root: root, Name: "Series A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("followed symlink outside root")
	}
	if err := deleteCleanupTree(cleanupTree{Root: root, Name: "../outside"}); err == nil {
		t.Fatal("accepted traversal")
	}
}

// Read-only verification against the current installation; never confirms a job.
func TestCleanupLocalPreview(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_CLEANUP_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in read-only audit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database unavailable")
	}
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api := &API{store: store, db: db}
	preview, err := api.cleanupPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Fingerprint) != 64 {
		t.Fatal("invalid fingerprint")
	}
	response := httptest.NewRecorder()
	api.libraryCleanup(response, httptest.NewRequest("GET", "/api/v1/library/cleanup", nil).WithContext(ctx))
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	rows, err := db.QueryContext(ctx, `EXPLAIN WITH candidate AS (SELECT id FROM tgstrm.library_cleanup_jobs WHERE status IN ('pending','running') AND (lease_until IS NULL OR lease_until<=now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE tgstrm.library_cleanup_jobs j SET status='running',lease_until=now()+interval '2 minutes' FROM candidate WHERE j.id=candidate.id RETURNING j.id::text,j.plan,j.completed_items`)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	// Reject outdated confirmation without creating any durable work.
	response = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/library/cleanup", strings.NewReader(`{"action":"clean","confirmation":"LIMPIAR","fingerprint":"outdated"}`)).WithContext(ctx)
	api.libraryCleanup(response, req)
	if response.Code != 409 {
		t.Fatal("accepted stale preview")
	}
}
