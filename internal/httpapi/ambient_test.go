package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAmbientTrendingValidationAndCache(t *testing.T) {
	a := &API{ambient: ambientCache{window: "week", expires: time.Now().Add(time.Hour), titles: []ambientTitle{{ID: 42, Title: "Example", MediaType: "movie", Backdrop: "/example.jpg"}}}}
	for _, path := range []string{"/api/v1/ambient/trending", "/api/v1/ambient/trending?window=week"} {
		w := httptest.NewRecorder()
		a.ambientTrending(w, httptest.NewRequest("GET", path, nil))
		var payload struct {
			Titles []ambientTitle `json:"titles"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &payload) != nil || len(payload.Titles) != 1 || payload.Titles[0].ID != 42 {
			t.Fatalf("cached response: %d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	a.ambientTrending(w, httptest.NewRequest("GET", "/api/v1/ambient/trending?window=invalid", nil))
	if w.Code != 422 {
		t.Fatalf("invalid period status: %d", w.Code)
	}
	req := httptest.NewRequest("GET", "http://localhost/api/v1/ambient/trending", nil)
	req.Header.Set("Origin", "https://foreign.example")
	w = httptest.NewRecorder()
	a.sameOrigin(a.ambientTrending)(w, req)
	if w.Code != 403 {
		t.Fatalf("foreign origin status: %d", w.Code)
	}
}
