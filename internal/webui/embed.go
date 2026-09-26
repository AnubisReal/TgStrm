// Package webui serves the compiled React application from the TgStrm binary.
//
// The release build copies web/dist into dist before compiling. Keeping the
// interface in the executable means production deployments have one origin for
// the panel, API and WebSocket and never need the Vite development server.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// dist/.keep lets Go tooling compile a source checkout before the frontend has
// been built. Release tooling replaces it with Vite's actual output.
//
//go:embed all:dist
var embedded embed.FS

// Handler serves the SPA for browser paths and delegates API and public stream
// requests to the supplied backend handler.
func Handler(backend http.Handler) http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/stream/") {
			backend.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		content, err := fs.ReadFile(assets, name)
		spa := false
		if err != nil {
			// Missing hashed assets must remain a real 404. Returning index.html
			// here hides broken releases and can make browsers cache HTML as JS.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			// React Router paths are not files. Serve the application shell so
			// bookmarked routes work after restarting the service.
			content, err = fs.ReadFile(assets, "index.html")
			name, spa = "index.html", true
		}
		if err != nil {
			http.Error(w, "TgStrm web interface is not bundled in this build", http.StatusServiceUnavailable)
			return
		}
		if spa || name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(w, r, name, zeroTime, bytes.NewReader(content))
	})
}
