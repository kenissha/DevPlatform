package gitemails

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes a person's own git-address list over HTTP, meant to
// be mounted by internal/server behind auth.RequireAuth only.
//
// Every handler acts on the caller's own JWT subject — no {subject} path
// parameter exists anywhere here, so nobody can read or edit someone
// else's list through these routes. That's also why none are
// Admin-gated: this is personal configuration, like internal/gittoken's
// /api/me routes, not an administrative one.
type Handlers struct {
	Store *Store
}

// listResponse is what the panel renders: the addresses already
// confirmed, plus the ones the git server saw on this person's pushes
// and is offering them to confirm with one click.
type listResponse struct {
	Claimed     []string `json:"claimed"`
	Suggestions []string `json:"suggestions"`
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
	h.writeList(w, user.Subject)
}

// ClaimMine handles POST /api/me/git-emails with {"email": "..."} —
// "this one is mine", whether it came from a suggestion or was typed in
// by hand. Responds with the caller's full updated lists so the panel
// re-renders without a second round trip.
func (h *Handlers) ClaimMine(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Store.Claim(user.Subject, req.Email); err != nil {
		if errors.Is(err, ErrInvalidEmail) {
			http.Error(w, "400 geçerli bir e-posta adresi girin", http.StatusBadRequest)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.writeList(w, user.Subject)
}

// DismissMine handles POST /api/me/git-emails/dismiss — "this one isn't
// mine", which has to be recorded rather than merely hidden, or the next
// push would offer the same address again.
func (h *Handlers) DismissMine(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Store.Dismiss(user.Subject, req.Email); err != nil {
		if errors.Is(err, ErrInvalidEmail) {
			http.Error(w, "400 geçerli bir e-posta adresi girin", http.StatusBadRequest)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.writeList(w, user.Subject)
}

// RemoveMine handles DELETE /api/me/git-emails?email=... — undoing a
// claim.
//
// The address travels as a query value rather than a path segment:
// addresses are user-supplied text, and IIS rejects some encoded
// characters in a path outright (see the branch-name lesson in
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

	if err := h.Store.Unclaim(user.Subject, email); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.writeList(w, user.Subject)
}

func (h *Handlers) writeList(w http.ResponseWriter, subject string) {
	claimed, err := h.Store.Claimed(subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	suggestions, err := h.Store.Suggestions(subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Claimed: claimed, Suggestions: suggestions})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
