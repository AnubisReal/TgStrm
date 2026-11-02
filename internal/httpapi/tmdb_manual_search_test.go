package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"tgstrm/internal/config"
	"time"
)

func TestManualTMDBSearchID(t *testing.T) {
	for _, tc := range []struct {
		query  string
		id     int64
		direct bool
	}{
		{"1396", 1396, true}, {" tmdb:1396 ", 1396, true}, {"TMDB: 1", 1, true},
		{"Breaking Bad", 0, false}, {"0", 0, false}, {"-1", 0, false}, {"1.5", 0, false},
		{"2147483648", 2147483647, false}, {"tmdb:abc", 0, false}, {"123abc", 0, false},
	} {
		id, direct := manualTMDBSearchID(tc.query)
		if direct != tc.direct || direct && id != tc.id {
			t.Fatalf("%q: %d %v", tc.query, id, direct)
		}
	}
}

func TestManualTMDBSearchDetailsIntegration(t *testing.T) {
	db := correctionsAuditDB(t)
	store, err := config.Open(os.Getenv("TGSTRM_TEST_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, store: store, client: &http.Client{Timeout: 12 * time.Second}}
	r := httptest.NewRequest("GET", "/api/v1/tmdb/search?media_type=tv&query=1396", nil)
	w := httptest.NewRecorder()
	a.searchTMDB(w, r)
	if w.Code != 200 {
		t.Fatalf("search failed: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Results []tmdbResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].ID != 1396 || result.Results[0].Title == "" {
		t.Fatalf("unexpected details: %+v", result)
	}
}
