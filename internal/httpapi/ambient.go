package httpapi

import (
	"net/http"
	"net/url"
	"sync"
	"time"
)

type ambientTitle struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	Overview  string `json:"overview"`
	Backdrop  string `json:"backdrop_path"`
	Poster    string `json:"poster_path"`
	MediaType string `json:"media_type"`
	Adult     bool   `json:"adult"`
}
type ambientCache struct {
	mu      sync.Mutex
	window  string
	expires time.Time
	titles  []ambientTitle
}

func (a *API) ambientTrending(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "week"
	}
	if window != "day" && window != "week" {
		fail(w, 422, "invalid_window", "Selecciona día o semana.")
		return
	}
	a.ambient.mu.Lock()
	defer a.ambient.mu.Unlock()
	if a.ambient.window == window && time.Now().Before(a.ambient.expires) {
		writeJSON(w, 200, map[string]any{"titles": a.ambient.titles})
		return
	}
	var response struct {
		Results []ambientTitle `json:"results"`
	}
	if err := a.tmdbRequestContext(r.Context(), "trending/all/"+window, url.Values{"language": {"es-ES"}}, &response); err != nil {
		fail(w, 502, "trending_unavailable", "No se pudieron cargar las tendencias de TMDB.")
		return
	}
	titles := make([]ambientTitle, 0, len(response.Results))
	for _, title := range response.Results {
		if !title.Adult && (title.MediaType == "movie" || title.MediaType == "tv") && title.Backdrop != "" {
			titles = append(titles, title)
		}
	}
	a.ambient.window = window
	a.ambient.expires = time.Now().Add(time.Hour)
	a.ambient.titles = titles
	writeJSON(w, 200, map[string]any{"titles": titles})
}
