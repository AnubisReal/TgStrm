package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"tgstrm/internal/config"
	"time"
)

// Opt-in provider audit. It only reads the catalogue and TMDB, never queues a send.
func TestMovieReadOnlyProviderAudit(t *testing.T) {
	if os.Getenv("TGSTRM_MOVIE_READONLY_AUDIT") != "1" {
		t.Skip("opt-in provider audit")
	}
	db := correctionsAuditDB(t)
	store, err := config.Open(os.Getenv("TGSTRM_TEST_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, store: store, client: &http.Client{Timeout: 12 * time.Second}}
	rows, err := db.Query(`SELECT coalesce(m.body,''),coalesce(mm.file_name,'') FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.messages m ON m.id=d.source_message_id JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE d.status='review' ORDER BY m.telegram_message_id DESC LIMIT 55`)
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		Body, File string
		Match      tmdbResult
		OK         bool
		Error      string
	}
	var items []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.Body, &v.File); err != nil {
			t.Fatal(err)
		}
		items = append(items, v)
	}
	rows.Close()
	successes := 0
	for i := range items {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		items[i].Match, items[i].OK, err = a.identifyMonitoredMovie(ctx, items[i].Body, items[i].File)
		cancel()
		if err != nil {
			items[i].Error = err.Error()
		}
		if items[i].OK {
			successes++
		}
		t.Logf("%d %v %s -> %s", i, items[i].OK, items[i].File, items[i].Match.Title)
	}
	output := os.Getenv("TGSTRM_MOVIE_AUDIT_OUTPUT")
	if output != "" {
		data, _ := json.MarshalIndent(items, "", "  ")
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("matched=%d total=%d", successes, len(items))
}
