// Package repoapi exposes repostore over HTTP: listing and creating
// repositories, and listing a repository's branches (needed by the merge
// request UI's source/target branch pickers). Actual repository content
// (clone/push/pull) still goes through internal/gitserver's smart-HTTP
// protocol — this package only covers the metadata a frontend needs to
// discover what exists.
package repoapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes the repository API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth (Create additionally
// behind auth.RequireRole(auth.RoleAdmin, ...) — creating a repository is
// an administrative action, same spirit as the design doc's Yönetici-only
// project setup).
type Handlers struct {
	Repos *repostore.Store
	// Audit is optional; a nil Logger records nothing (see internal/audit).
	Audit *audit.Logger
	// Access is optional; a nil Store means every caller sees every repo
	// (see internal/access). When set, List narrows its response to what
	// the caller is allowed to see — the one place a restricted repo must
	// never appear for a restricted subject, everything else (Branches,
	// and every other repo-scoped endpoint elsewhere in this codebase)
	// enforces the same restriction via access.RequireRepoAccess at the
	// router level instead, since they operate on a single named repo
	// rather than a list.
	Access *access.Store
}

// List handles GET /api/repos.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	names, err := h.Repos.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok && user.Role != auth.RoleAdmin {
		names, err = h.Access.FilterRepos(user.Subject, names)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	sort.Strings(names)
	writeJSON(w, http.StatusOK, names)
}

type createRequest struct {
	Name string `json:"name"`
}

// Create handles POST /api/repos. Mount this behind
// auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does not
// check the caller's role.
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "400 name is required", http.StatusBadRequest)
		return
	}

	if _, err := h.Repos.Create(req.Name); err != nil {
		switch {
		case errors.Is(err, repostore.ErrInvalidName):
			http.Error(w, "400 invalid repository name", http.StatusBadRequest)
		case errors.Is(err, repostore.ErrAlreadyExists):
			http.Error(w, "409 repository already exists", http.StatusConflict)
		default:
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// A failed audit write must not fail the action it records — the repo
	// already exists on disk by this point, so erroring here would report
	// a failure that didn't happen.
	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionRepoCreated, req.Name, req.Name, "Repo oluşturuldu")
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
}

// Branches handles GET /api/repos/{repo}/branches.
func (h *Handlers) Branches(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		if errors.Is(err, repostore.ErrNotExist) || errors.Is(err, repostore.ErrInvalidName) {
			http.Error(w, "404 repository not found", http.StatusNotFound)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	iter, err := gitRepo.Branches()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer iter.Close()

	names := []string{}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	sort.Strings(names)

	writeJSON(w, http.StatusOK, names)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
