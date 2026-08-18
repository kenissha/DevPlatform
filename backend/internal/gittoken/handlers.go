package gittoken

import (
	"encoding/json"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes Store's operations as http.HandlerFuncs — see
// cmd/devplatform/main.go and internal/server for how they're mounted.
type Handlers struct {
	Store *Store
}

// GenerateMine handles POST /api/me/git-token, meant to be mounted
// behind auth.RequireAuth only (no role requirement — anyone can mint
// their own key). It always acts on the caller's own JWT subject; there
// is deliberately no path parameter, so nobody can request a token on
// someone else's behalf through this endpoint.
func (h *Handlers) GenerateMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := h.Store.Generate(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// Revoke handles DELETE /api/git-token/{subject}, meant to be mounted
// behind auth.RequireRole(auth.RoleAdmin, ...) — it revokes someone
// else's token, mirroring internal/access.Handlers.Clear's admin-only
// pattern for /api/access/{subject}.
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Revoke(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
