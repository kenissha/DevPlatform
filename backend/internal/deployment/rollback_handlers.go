package deployment

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// releaseInfo is one release still on disk for a (repo, environment) —
// what Releases lists and the rollback picker in the panel renders. Only
// the release's own directory name is exposed, never its full server
// path, matching FailureReason's own reasoning elsewhere in this package
// for keeping absolute filesystem paths out of API responses.
type releaseInfo struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// Releases handles GET /api/repos/{repo}/deployments/{environment}/releases,
// listing every release still on disk for (repo, environment), newest
// first, with the one IIS is currently serving marked active. Mount this
// behind auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does
// not check the caller's role.
func (h *Handlers) Releases(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	if h.Versions == nil {
		http.Error(w, "500 rollback is not configured on this server", http.StatusInternalServerError)
		return
	}
	if _, err := h.Targets.Find(repo, environment); err != nil {
		http.Error(w, "400 no deploy target configured for this repo and environment", http.StatusBadRequest)
		return
	}

	paths, err := h.Versions.List(repo, environment)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	reqs, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	active := activeRelease(reqs, environment)

	releases := make([]releaseInfo, len(paths))
	for i, p := range paths {
		releases[i] = releaseInfo{Name: filepath.Base(p), Active: p == active}
	}
	writeJSON(w, http.StatusOK, releases)
}

// activeRelease returns the ReleaseDir of (repo, environment)'s most
// recently completed deploy or rollback, or "" if none has ever
// succeeded. reqs must already be sorted newest-first, Store.List's own
// contract — the same list this package's other cross-request views
// (ListAll) already rely on being pre-sorted.
func activeRelease(reqs []Request, environment string) string {
	for _, req := range reqs {
		if req.Environment == environment && req.Status == StatusDeployed && req.ReleaseDir != "" {
			return req.ReleaseDir
		}
	}
	return ""
}

// rollbackRequest is the body POST
// /api/repos/{repo}/deployments/{environment}/rollback expects — just the
// release directory name Releases listed, never a caller-supplied path:
// Rollback re-resolves it against Versions.List's own trusted output
// below, so nothing this handler ever passes to appcmd originates as free
// text from an HTTP body.
type rollbackRequest struct {
	Release string `json:"release"`
}

// Rollback handles POST
// /api/repos/{repo}/deployments/{environment}/rollback: points the
// target's IIS site back at an existing, already-built release directory
// still on disk. Unlike Approve, this is deliberately synchronous and
// immediate with no pending/review stage — no checkout, no build, no
// secrets rewrite, just the one appcmd call Approve's last step also
// makes — because a rollback exists to be the fast, one-click response to
// a bad release already live, and requiring a second admin's approval
// would defeat that (the admin doing the rollback is already the
// authority the ordinary approve step would have asked for). Mount this
// behind auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does
// not check the caller's role.
func (h *Handlers) Rollback(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	if h.Versions == nil || h.IIS == nil {
		http.Error(w, "500 rollback is not configured on this server", http.StatusInternalServerError)
		return
	}

	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Release == "" {
		http.Error(w, "400 release is required", http.StatusBadRequest)
		return
	}

	target, err := h.Targets.Find(repo, environment)
	if err != nil {
		http.Error(w, "400 no deploy target configured for this repo and environment", http.StatusBadRequest)
		return
	}

	paths, err := h.Versions.List(repo, environment)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	releaseDir := ""
	for _, p := range paths {
		if filepath.Base(p) == req.Release {
			releaseDir = p
			break
		}
	}
	if releaseDir == "" {
		http.Error(w, "400 that release does not exist", http.StatusBadRequest)
		return
	}
	// paths came from Versions.List a moment ago, but a nightly prune or a
	// concurrent deploy could have deleted it since — Stat here rather
	// than trusting the listing, so a stale click on an already-pruned
	// release gets a clear 400 instead of appcmd pointing IIS at a
	// directory that no longer exists.
	if _, err := os.Stat(releaseDir); err != nil {
		http.Error(w, "400 that release no longer exists on disk", http.StatusBadRequest)
		return
	}

	if err := h.IIS.SetPhysicalPath(target.SiteName, releaseDir); err != nil {
		log.Printf("deployment: rollback failed for %s/%s to %q: %v", repo, environment, releaseDir, err)
		http.Error(w, "500 rollback failed", http.StatusInternalServerError)
		return
	}

	created, err := h.Store.CreateRollback(repo, environment, releaseDir, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log(user.Subject, audit.ActionDeploymentRollback, repo, created.ID,
		"Rollback: "+repo+" → "+environment+" ("+filepath.Base(releaseDir)+")")

	writeJSON(w, http.StatusOK, created)
}
