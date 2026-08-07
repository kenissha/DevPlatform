package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// NewRouter builds the top-level HTTP router. gitHandler serves the git
// smart-HTTP protocol under its own prefix (see internal/gitserver). Later
// packages extend this with additional routes (task API, ...).
func NewRouter(gitHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	// The "/git/" prefix here must stay in sync with gitserver.Prefix
	// ("/git") — see internal/gitserver.NewHandler's doc comment.
	mux.Handle("/git/", gitHandler)
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handleHealth: failed to encode response: %v", err)
	}
}
