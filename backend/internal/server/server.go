package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// NewRouter builds the top-level HTTP router. gitHandler serves the git
// smart-HTTP protocol under its own prefix (see internal/gitserver).
// authMiddleware wraps routes that require a valid JWT (see internal/auth);
// /healthz and the git routes are unaffected by it. Later packages extend
// this with additional routes (task API, ...).
func NewRouter(gitHandler http.Handler, authMiddleware func(http.Handler) http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	// The "/git/" prefix here must stay in sync with gitserver.Prefix
	// ("/git") — see internal/gitserver.NewHandler's doc comment.
	mux.Handle("/git/", gitHandler)
	// /api/me is the minimal proof that a JWT issued by the external
	// identity system round-trips correctly through internal/auth end to
	// end; later task-board/merge-request routes mount alongside it under
	// the same authMiddleware.
	mux.Handle("GET /api/me", authMiddleware(http.HandlerFunc(handleMe)))
	return mux
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("handleMe: failed to encode response: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handleHealth: failed to encode response: %v", err)
	}
}
