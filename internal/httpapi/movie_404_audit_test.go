package httpapi

import (
	"context"
	"net/http"
	"os"
	"testing"
	"tgstrm/internal/config"
	"time"
)

func TestMovie404ReadOnlyAudit(t *testing.T) {
	if os.Getenv("TGSTRM_MOVIE_404_AUDIT") != "1" {
		t.Skip("read-only opt-in")
	}
	db := correctionsAuditDB(t)
	store, err := config.Open(os.Getenv("TGSTRM_TEST_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, store: store, client: &http.Client{Timeout: 12 * time.Second, Transport: movieAuditTransport(func(r *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(r)
		if response != nil && response.StatusCode != 200 {
			t.Logf("provider path=%s status=%d", r.URL.Path, response.StatusCode)
		}
		return response, err
	})}}
	rows, err := db.Query(`SELECT coalesce(m.body,''),coalesce(mm.file_name,'') FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.messages m ON m.id=d.source_message_id JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE d.last_error='TMDB returned status 404'`)
	if err != nil {
		t.Fatal(err)
	}
	var inputs [][2]string
	for rows.Next() {
		var v [2]string
		if err = rows.Scan(&v[0], &v[1]); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, v)
	}
	rows.Close()
	for _, v := range inputs {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		match, ok, err := a.identifyMonitoredMovie(ctx, v[0], v[1])
		cancel()
		t.Logf("file=%s match=%d ok=%v err=%v", v[1], match.ID, ok, err)
	}
}
