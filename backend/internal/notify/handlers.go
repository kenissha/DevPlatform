package notify

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes the notifications API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth. Task 2's trigger
// wiring calls Store.Create directly rather than through Handlers — the
// same way taskboard.Handlers.Create calls Store.Create directly instead
// of another HTTP round-trip.
type Handlers struct {
	Store *Store
}

// List handles GET /api/notifications — only the authenticated user's own.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	notifications, err := h.Store.ListForUser(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, notifications)
}

// MarkRead handles POST /api/notifications/{id}/read.
func (h *Handlers) MarkRead(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	if err := h.Store.MarkRead(user.Subject, id); err != nil {
		h.writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "404 notification not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidRecipient), errors.Is(err, ErrInvalidID):
		http.Error(w, "400 Bad Request", http.StatusBadRequest)
	default:
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
