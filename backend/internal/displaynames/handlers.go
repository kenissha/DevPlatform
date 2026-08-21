package displaynames

import (
	"encoding/json"
	"net/http"
)

// Handlers exposes Store's admin operations as http.HandlerFuncs, meant to
// be mounted by internal/server entirely behind
// auth.RequireRole(auth.RoleAdmin, ...) — same posture as
// internal/access.Handlers, since only an admin should be renaming how
// someone else appears in the panel.
type Handlers struct {
	Store *Store
}

// List handles GET /api/display-names, returning every subject that has an
// override configured. A subject absent from the response falls back to
// their email in the panel — see Store's doc comment.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	registry, err := h.Store.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, registry)
}

type setRequest struct {
	Name string `json:"name"`
}

// Set handles PUT /api/display-names/{subject}, setting subject's
// display-name override to the request body's name.
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

	if err := h.Store.Set(subject, req.Name); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// Clear handles DELETE /api/display-names/{subject}, removing subject's
// override so the panel falls back to their email again.
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
