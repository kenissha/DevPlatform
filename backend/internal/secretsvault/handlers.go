package secretsvault

import (
	"encoding/json"
	"net/http"
)

// Handlers exposes Store's write path as an HTTP endpoint, meant to be
// mounted by internal/server entirely behind
// auth.RequireRole(auth.RoleAdmin, ...) — same posture as
// internal/access.Handlers and internal/displaynames.Handlers.
//
// Deliberately write-only: there is no List or Get handler here, and
// there never should be. Store.Get exists only for deploy.Pipeline's own
// internal use when injecting a secret into a release — exposing it over
// HTTP would let anyone with panel access read back a plaintext secret
// (an OAS password, a JWT signing key) that was only ever meant to be
// written once and re-used silently on every deploy. This mirrors how
// GitHub Actions' own environment secrets work: set or overwrite, never
// view again.
type Handlers struct {
	Store *Store
}

type setSecretsRequest struct {
	Content string `json:"content"`
}

// Set handles PUT /api/secrets/{repo}/{environment}, encrypting the
// request body's content and storing it for (repo, environment) —
// overwriting whatever was there before. Responds 204 (not 200) on
// success: the frontend's request() helper treats 204 as "no body to
// parse" and only that status, matching clearAccess/clearDisplayName's
// DELETE handlers — a 200 with an empty body makes it try to
// JSON-decode nothing and throw. The response never includes the
// content back, even on success.
func (h *Handlers) Set(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, "503 secrets vault is not configured (DEVPLATFORM_SECRETS_KEY is not set)", http.StatusServiceUnavailable)
		return
	}

	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if repo == "" || environment == "" {
		http.Error(w, "400 repo and environment are required", http.StatusBadRequest)
		return
	}

	var req setSecretsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	if err := h.Store.Put(repo, environment, []byte(req.Content)); err != nil {
		if err == ErrInvalidRepo {
			http.Error(w, "400 invalid repo or environment name", http.StatusBadRequest)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
