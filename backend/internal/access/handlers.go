package access

import (
	"encoding/json"
	"net/http"
)

// Handlers exposes Store's admin operations as http.HandlerFuncs, meant to
// be mounted by internal/server entirely behind
// auth.RequireRole(auth.RoleAdmin, ...) — every operation here changes or
// reveals someone else's access, never the caller's own.
type Handlers struct {
	Store *Store
}

// List handles GET /api/access, returning every currently-restricted
// subject and their allow-list. A subject absent from the response is
// unrestricted (sees every repository) — see Store's doc comment.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	registry, err := h.Store.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, registry)
}

type setRequest struct {
	Repos []string `json:"repos"`
}

// Set handles PUT /api/access/{subject}, restricting subject to exactly
// the repos in the request body. An empty "repos" array restricts subject
// to nothing — use DELETE /api/access/{subject} instead to remove the
// restriction entirely.
func (h *Handlers) Set(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	if err := h.Store.Set(subject, req.Repos); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"repos": req.Repos})
}

// Clear handles DELETE /api/access/{subject}, removing any restriction on
// subject and returning them to unrestricted (sees every repository).
func (h *Handlers) Clear(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Clear(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
