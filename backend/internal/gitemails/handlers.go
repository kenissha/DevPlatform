package gitemails

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes a person's own git-email list over HTTP, meant to be
// mounted by internal/server behind auth.RequireAuth only.
//
// Every handler here acts on the caller's own JWT subject — there is no
// {subject} path parameter anywhere, so nobody can read or edit someone
// else's list through these routes. That's also why none of them are
// Admin-gated: this is personal configuration, like internal/gittoken's
// own /api/me routes, not an administrative one.
type Handlers struct {
	Store *Store
}

type emailRequest struct {
	Email string `json:"email"`
}

// ListMine handles GET /api/me/git-emails.
func (h *Handlers) ListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	emails, err := h.Store.List(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, emails)
}

// AddMine handles POST /api/me/git-emails with {"email": "..."}, and
// responds with the caller's full updated list so the panel doesn't
// need a second round trip to re-render.
func (h *Handlers) AddMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req emailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	if err := h.Store.Add(user.Subject, req.Email); err != nil {
		if errors.Is(err, ErrInvalidEmail) {
			http.Error(w, "400 geçerli bir e-posta adresi girin", http.StatusBadRequest)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.ListMine(w, r)
}

// RemoveMine handles DELETE /api/me/git-emails?email=...
//
// The address travels as a query value rather than a path segment:
// addresses are user-supplied text, and a path segment would have to
// survive URL-encoding through IIS, which rejects some encoded
// characters in paths outright (see the branch-name lesson in
// docs/DURUM.md's 2026-09-04 entry).
func (h *Handlers) RemoveMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "400 email query parameter is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Remove(user.Subject, email); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.ListMine(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
