package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves an embedded SPA. Static files are served directly;
// all other paths fall back to index.html for client-side routing.
func spaHandler(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(fsys))

	return func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Open.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to open the file. If it exists, serve it.
		f, err := fsys.Open(path)
		if err == nil {
			f.Close()
			setCacheHeaders(w, path)
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for client-side routing.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

func setCacheHeaders(w http.ResponseWriter, path string) {
	switch {
	case strings.HasPrefix(path, "assets/"):
		// Vite hashed assets are immutable.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case path == "sw.js" || strings.HasPrefix(path, "workbox-"):
		// Service worker files must not be cached.
		w.Header().Set("Cache-Control", "no-cache")
	case path == "index.html":
		w.Header().Set("Cache-Control", "no-cache")
	}
}
