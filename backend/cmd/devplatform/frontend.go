package main

import (
	"net/http"
	"os"
	"path/filepath"
)

// frontendHandler serves dir (the built frontend's static files, e.g.
// frontend/dist after `npm run build`) as a single-page app: a request for
// a path that isn't a real file in dir falls back to index.html instead of
// a 404, so client-side routes (e.g. /repos/foo) still load on a direct
// visit or a page refresh — the standard pattern for serving a Vite/React
// Router build without a dedicated web server or framework.
func frontendHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if _, err := os.Stat(requested); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
