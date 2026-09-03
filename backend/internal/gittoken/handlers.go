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

type generateRequest struct {
	Label string `json:"label"`
}

// GenerateMine handles POST /api/me/git-token, meant to be mounted
// behind auth.RequireAuth only (no role requirement — anyone can mint
// their own key). It always acts on the caller's own JWT subject; there
// is deliberately no path parameter, so nobody can request a token on
// someone else's behalf through this endpoint. An empty or missing
// "label" is accepted, not an error — the panel and the CLI login tool
// always supply one, but this shouldn't be the reason a bare
// `curl -X POST` fails.
func (h *Handlers) GenerateMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req generateRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	id, token, err := h.Store.Generate(user.Subject, req.Label)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "token": token})
}

// ListMine handles GET /api/me/git-tokens — returns the caller's own
// active tokens (id, label, createdAt — never a hash).
func (h *Handlers) ListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	tokens, err := h.Store.List(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokens)
}

// RevokeMine handles DELETE /api/me/git-tokens/{id} — revokes one of
// the caller's own tokens. Always acts on the caller's own subject
// (from the JWT, not a path parameter), so nobody can revoke someone
// else's token through this endpoint — that's what the admin-only
// Revoke below is for.
func (h *Handlers) RevokeMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "400 id is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Revoke(user.Subject, id); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Revoke handles DELETE /api/git-token/{subject}, meant to be mounted
// behind auth.RequireRole(auth.RoleAdmin, ...) — revokes EVERY one of
// subject's tokens (see Store.RevokeAll), the "cut off this person's
// git access entirely" admin action, mirroring
// internal/access.Handlers.Clear's admin-only pattern for
// /api/access/{subject}.
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.RevokeAll(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
