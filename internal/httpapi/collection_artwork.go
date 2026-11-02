package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	telegramauth "tgstrm/internal/telegram"
)

// One shared discovery snapshot prevents each cabinet from downloading the same pages.
type cabinetArtworkCache struct {
	mu      sync.Mutex
	expires time.Time
	pages   map[string]int
	titles  map[string][]cabinetTitle
	retry   map[string]time.Time
}
type cabinetTitle struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Title        string `json:"title"`
	Poster       string `json:"poster_path"`
}

var cabinetPosterPath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png)$`)

func cabinetInitial(title string) string {
	title = strings.TrimSpace(title)
	for _, r := range title {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		r = unicode.ToUpper(r)
		if r >= '0' && r <= '9' {
			return "0-9"
		}
		normalized := strings.NewReplacer("Á", "A", "À", "A", "Â", "A", "Ä", "A", "É", "E", "È", "E", "Ê", "E", "Ë", "E", "Í", "I", "Ì", "I", "Î", "I", "Ï", "I", "Ó", "O", "Ò", "O", "Ô", "O", "Ö", "O", "Ú", "U", "Ù", "U", "Û", "U", "Ü", "U").Replace(string(r))
		if strings.Contains("ABCDEFGHIJKLMNÑOPQRSTUVWXYZ", normalized) {
			return normalized
		}
		return "OTHER"
	}
	return "OTHER"
}

func cabinetPosters(titles []cabinetTitle, kind, bucket string) []string {
	paths := []string{}
	seen := map[int64]bool{}
	for _, title := range titles {
		name := title.Name
		if kind == "movies" {
			name = title.Title
		}
		if name == "" {
			name = title.OriginalName
		}
		if bucket != "" && cabinetInitial(name) != bucket {
			continue
		}
		if seen[title.ID] || !cabinetPosterPath.MatchString(title.Poster) {
			continue
		}
		seen[title.ID] = true
		paths = append(paths, title.Poster)
		if len(paths) == 12 {
			break
		}
	}
	return paths
}

func (a *API) collectionArtwork(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var kind, bucket string
	err := a.db.QueryRowContext(r.Context(), `SELECT cg.media_kind,cg.bucket FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id WHERE cg.collection_id::text=$1 AND cg.id::text=$2 AND cg.account_id=$3 AND lc.state<>'archived' AND cg.state<>'archived'`, r.PathValue("collectionID"), r.PathValue("memberID"), telegramauth.CatalogAccountID).Scan(&kind, &bucket)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, 500, "artwork_unavailable", "No se pudo cargar la imagen.")
		return
	}
	path, err := a.ensureCabinetArtwork(r.Context(), kind, bucket)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	serveCabinetArtwork(w, r, path)
}

func (a *API) ensureCabinetArtwork(ctx context.Context, kind, bucket string) (string, error) {
	var err error
	if (kind != "series" && kind != "movies") || (kind == "series" && bucket == "") {
		return "", errors.New("imagen de cajón no disponible")
	}
	if kind == "movies" {
		bucket = ""
	}
	// Filenames contain only fixed kinds and normalized alphabet buckets.
	if bucket != "" && bucket != "0-9" && bucket != "OTHER" && (len([]rune(bucket)) != 1 || !strings.Contains("ABCDEFGHIJKLMNÑOPQRSTUVWXYZ", bucket)) {
		return "", errors.New("imagen de cajón no disponible")
	}
	path := filepath.Join(a.store.Dir(), "cache", "cabinet-artwork", fmt.Sprintf("v1-%s-%s.jpg", kind, bucket))
	// A ready avatar never waits behind discovery for a different letter.
	if stat, e := os.Stat(path); e == nil && time.Since(stat.ModTime()) < 7*24*time.Hour {
		return path, nil
	}
	for !a.cabinetArtwork.mu.TryLock() {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer a.cabinetArtwork.mu.Unlock()
	if stat, e := os.Stat(path); e == nil && time.Since(stat.ModTime()) < 7*24*time.Hour {
		return path, nil
	}
	key := kind + ":" + bucket
	if time.Now().Before(a.cabinetArtwork.retry[key]) {
		return "", errors.New("imagen de cajón no disponible")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cache := &a.cabinetArtwork
	if time.Now().After(cache.expires) {
		cache.pages = map[string]int{}
		cache.retry = map[string]time.Time{}
		cache.titles = map[string][]cabinetTitle{}
		cache.expires = time.Now().Add(24 * time.Hour)
	}
	posters := cabinetPosters(cache.titles[kind], kind, bucket)
	endpoint := "discover/tv"
	if kind == "movies" {
		endpoint = "discover/movie"
	}
	for len(posters) < 12 && cache.pages[kind] < 40 {
		page := cache.pages[kind] + 1
		var response struct {
			Results    []cabinetTitle `json:"results"`
			TotalPages int            `json:"total_pages"`
		}
		query := url.Values{"language": {"es-ES"}, "sort_by": {"popularity.desc"}, "include_adult": {"false"}, "page": {fmt.Sprint(page)}}
		if err = a.tmdbRequestContext(ctx, endpoint, query, &response); err != nil {
			break
		}
		cache.pages[kind] = page
		cache.titles[kind] = append(cache.titles[kind], response.Results...)
		posters = cabinetPosters(cache.titles[kind], kind, bucket)
		if page >= response.TotalPages || len(response.Results) == 0 {
			cache.pages[kind] = 40
			break
		}
	}
	images := []image.Image{}
	for _, poster := range posters {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://image.tmdb.org/t/p/w185"+poster, nil)
		if e != nil {
			continue
		}
		resp, e := a.client.Do(req)
		if e != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		raw, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
		resp.Body.Close()
		if e != nil || len(raw) > 2*1024*1024 {
			continue
		}
		config, _, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || config.Width > 1024 || config.Height > 2048 {
			continue
		}
		im, _, e := image.Decode(bytes.NewReader(raw))
		if e == nil {
			images = append(images, im)
		}
	}
	if len(images) == 0 {
		cache.retry[key] = time.Now().Add(10 * time.Minute)
		if _, e := os.Stat(path); e == nil {
			return path, nil
		}
		return "", errors.New("imagen de cajón no disponible")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", errors.New("no se pudo guardar la imagen del cajón")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".artwork-*.jpg")
	if err != nil {
		return "", errors.New("no se pudo guardar la imagen del cajón")
	}
	temp := file.Name()
	defer os.Remove(temp)
	err = jpeg.Encode(file, renderCabinetMosaic(images), &jpeg.Options{Quality: 90})
	closeErr := file.Close()
	if err != nil || closeErr != nil || os.Rename(temp, path) != nil {
		return "", errors.New("no se pudo guardar la imagen del cajón")
	}
	return path, nil
}

func serveCabinetArtwork(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

// Inverse projection fills the square without exposing corners. Each column is
// staggered; the plane tilts clockwise and approaches the viewer at the bottom.
func renderCabinetMosaic(images []image.Image) *image.RGBA {
	const size = 512
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	if len(images) == 0 {
		return out
	}
	angle := 14 * math.Pi / 180
	c, s := math.Cos(angle), math.Sin(angle)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-256, float64(y)-256
			u, v := c*dx+s*dy, -s*dx+c*dy
			perspective := 1 + v/2200
			u = u/perspective + 650
			v = v + 650
			col := int(math.Floor(u / 112))
			if col%2 != 0 {
				v += 81
			}
			row := int(math.Floor(v / 166))
			tx, ty := u-float64(col*112), v-float64(row*166)
			if tx < 4 || ty < 4 {
				out.SetRGBA(x, y, color.RGBA{12, 15, 18, 255})
				continue
			}
			index := (col*5 + row) % len(images)
			if index < 0 {
				index += len(images)
			}
			im := images[index]
			b := im.Bounds()
			px := b.Min.X + int((tx-4)/108*float64(b.Dx()))
			py := b.Min.Y + int((ty-4)/162*float64(b.Dy()))
			out.Set(x, y, im.At(px, py))
		}
	}
	return out
}
