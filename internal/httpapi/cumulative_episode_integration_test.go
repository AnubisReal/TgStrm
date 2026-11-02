package httpapi

import (
	"context"
	"net/http"
	"os"
	"testing"
	"tgstrm/internal/config"
	"time"
)

// Explicit opt-in: reads the requested topic and caches official season metadata;
// never edits Telegram, episode assignments, or exports.
func TestAuditAidaCumulativeEpisodes(t *testing.T) {
	if os.Getenv("TGSTRM_AUDIT_AIDA") != "1" {
		t.Skip("explicit topic audit")
	}
	db := correctionsAuditDB(t)
	store, err := config.Open(os.Getenv("TGSTRM_TEST_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, store: store, client: &http.Client{Timeout: 12 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT m.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,'') FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE m.topic_id=4100462 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct{ id, file, caption string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.file, &e.caption); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	changes, unresolved := 0, 0
	for _, e := range entries {
		ep, ok := a.detectSeriesEpisode(ctx, 1032, e.file, e.caption)
		old, oldOK := detectEpisode(e.file, e.caption)
		if !ok {
			unresolved++
			t.Logf("REVIEW %s %s", e.id, e.file)
			continue
		}
		if !oldOK || ep.Season != old.Season || ep.Episode != old.Episode {
			changes++
			t.Logf("CORRECT %s %s => %s", e.id, e.file, ep.code())
		}
	}
	t.Logf("AUDIT files=%d changes=%d unresolved=%d", len(entries), changes, unresolved)
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
}
