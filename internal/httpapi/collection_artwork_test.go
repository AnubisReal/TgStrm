package httpapi

import (
	"context"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	"time"
)

func TestCabinetAlphabetAndPosterSelection(t *testing.T) {
	for name, want := range map[string]string{"Águila roja": "A", "Ña": "Ñ", "1899": "0-9", "東京": "OTHER", "— Arrow": "A", "": "OTHER"} {
		if got := cabinetInitial(name); got != want {
			t.Fatalf("%q: got %q want %q", name, got, want)
		}
	}
	titles := []cabinetTitle{
		{ID: 1, Name: "Arrow", Poster: "/arrow.jpg"},
		{ID: 1, Name: "Arrow", Poster: "/duplicate.jpg"},
		{ID: 2, Name: "Breaking Bad", Poster: "/breaking.jpg"},
		{ID: 3, Name: "Ahsoka", Poster: "/ahsoka.jpg"},
		{ID: 4, Name: "A", Poster: "/../../secret.jpg"},
	}
	paths := cabinetPosters(titles, "series", "A")
	if len(paths) != 2 || paths[0] != "/arrow.jpg" || paths[1] != "/ahsoka.jpg" {
		t.Fatalf("selection: %v", paths)
	}
}

func TestCabinetMosaicFillsSquare(t *testing.T) {
	poster := image.NewRGBA(image.Rect(0, 0, 185, 278))
	for y := 0; y < 278; y++ {
		for x := 0; x < 185; x++ {
			poster.SetRGBA(x, y, color.RGBA{210, 100, 45, 255})
		}
	}
	out := renderCabinetMosaic([]image.Image{poster})
	if out.Bounds() != image.Rect(0, 0, 512, 512) {
		t.Fatal(out.Bounds())
	}
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			if out.RGBAAt(x, y).A != 255 {
				t.Fatalf("Unfilled pixel %d,%d", x, y)
			}
		}
	}
}

// Explicit local audit: reads the existing catalog and writes only the image cache.
func TestCabinetArtworkLocal(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_ARTWORK_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in TMDB artwork audit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
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
	var collection, member string
	err = db.QueryRowContext(ctx, `SELECT collection_id::text,id::text FROM tgstrm.collection_groups WHERE state<>'archived' AND media_kind='series' AND bucket='A' LIMIT 1`).Scan(&collection, &member)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{store: store, db: db, client: &http.Client{Timeout: 12 * time.Second}}
	req := httptest.NewRequest("GET", "/artwork", nil).WithContext(ctx)
	req.SetPathValue("collectionID", collection)
	req.SetPathValue("memberID", member)
	response := httptest.NewRecorder()
	api.collectionArtwork(response, req)
	if response.Code != 200 {
		t.Fatalf("artwork response: %d", response.Code)
	}
	if response.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("wrong media type")
	}
	decoded, _, err := image.Decode(response.Body)
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 512, 512) {
		t.Fatal("invalid cached mosaic")
	}
}

func TestCabinetPhotoQueueSQL(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_ARTWORK_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in database audit")
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var migrated bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='tgstrm' AND table_name='collection_groups' AND column_name='photo_status')`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if !migrated {
		migration, err := os.ReadFile("../../migrations/045_collection_group_photos.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	// EXPLAIN validates claim SQL without changing or sending anything.
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+collectionGroupPhotoClaimSQL)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.collection_groups WHERE photo_status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
}
