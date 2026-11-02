package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

var ErrMoviePosterPreparation = errors.New("no se pudo preparar el póster")

var moviePosterClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

// Only official TMDB poster paths are accepted; no arbitrary URL or disk cache.
func fetchMoviePoster(ctx context.Context, client *http.Client, path string) ([]byte, error) {
	if !tmdbPosterPath.MatchString(path) {
		return nil, errors.New("falta un póster TMDB válido")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://image.tmdb.org/t/p/w780"+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("no se pudo descargar el póster de TMDB; se reintentará")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("TMDB no devolvió el póster; se reintentará")
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) == 0 || len(data) > limit {
		return nil, errors.New("el póster está vacío, incompleto o supera el tamaño permitido")
	}
	if http.DetectContentType(data) != "image/jpeg" && http.DetectContentType(data) != "image/png" {
		return nil, errors.New("TMDB no devolvió una imagen de póster válida")
	}
	return data, nil
}
