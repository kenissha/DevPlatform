package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
)

// NewRouter builds the top-level HTTP router. gitHandler serves the git
// smart-HTTP protocol under its own prefix (see internal/gitserver).
// authMiddleware wraps routes that require a valid JWT (see internal/auth);
// /healthz and the git routes are unaffected by it. mr provides the merge
// request review API (see internal/mergerequest). Later packages extend
// this with additional routes (task board, ...).
func NewRouter(gitHandler http.Handler, authMiddleware func(http.Handler) http.Handler, mr *mergerequest.Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	// The "/git/" prefix here must stay in sync with gitserver.Prefix
	// ("/git") — see internal/gitserver.NewHandler's doc comment.
	mux.Handle("/git/", gitHandler)
	// /api/me is the minimal proof that a JWT issued by the external
	// identity system round-trips correctly through internal/auth end to
	// end.
	mux.Handle("GET /api/me", authMiddleware(http.HandlerFunc(handleMe)))

	// Any authenticated user (Developer or Admin) can open a merge request
	// or read its diff; only an Admin can approve/reject it — "kör onay
	// yoktur" is enforced by Get always recomputing the diff live, and
	// approve/reject being gated separately from create/list.
	mux.Handle("POST /api/repos/{repo}/merge-requests", authMiddleware(http.HandlerFunc(mr.Create)))
	mux.Handle("GET /api/repos/{repo}/merge-requests", authMiddleware(http.HandlerFunc(mr.List)))
	mux.Handle("GET /api/repos/{repo}/merge-requests/{id}", authMiddleware(http.HandlerFunc(mr.Get)))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/approve",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(mr.Approve))))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/reject",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(mr.Reject))))

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
