package mergerequest

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes the merge request API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth (and, for
// Approve/Reject, auth.RequireRole(auth.RoleAdmin, ...)).
type Handlers struct {
	Store *Store
	Repos *repostore.Store
}

type createRequest struct {
	Title        string `json:"title"`
	SourceBranch string `json:"sourceBranch"`
	TargetBranch string `json:"targetBranch"`
}

// Create handles POST /api/repos/{repo}/merge-requests.
func (h *Handlers) Create(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.SourceBranch == "" || req.TargetBranch == "" {
		http.Error(w, "400 title, sourceBranch, and targetBranch are required", http.StatusBadRequest)
		return
	}
	if req.SourceBranch == req.TargetBranch {
		http.Error(w, "400 sourceBranch and targetBranch must differ", http.StatusBadRequest)
		return
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	if _, err := resolveBranchTip(gitRepo, req.SourceBranch); err != nil {
		h.writeBranchError(w, err)
		return
	}
	if _, err := resolveBranchTip(gitRepo, req.TargetBranch); err != nil {
		h.writeBranchError(w, err)
		return
	}

	mr, err := h.Store.Create(repo, req.Title, req.SourceBranch, req.TargetBranch, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, mr)
}

// List handles GET /api/repos/{repo}/merge-requests.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	mrs, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, mrs)
}

type mergeRequestDetail struct {
	MergeRequest
	Diff DiffResult `json:"diff"`
}

// Get handles GET /api/repos/{repo}/merge-requests/{id}, returning the
// merge request together with its computed diff — the review screen's
// data source.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	mr, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	diff, err := Diff(gitRepo, mr.TargetBranch, mr.SourceBranch)
	if err != nil {
		h.writeBranchError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mergeRequestDetail{MergeRequest: mr, Diff: diff})
}

// Approve handles POST /api/repos/{repo}/merge-requests/{id}/approve.
// Mount this behind auth.RequireRole(auth.RoleAdmin, ...) — this handler
// itself does not check the caller's role.
func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, StatusApproved)
}

// Reject handles POST /api/repos/{repo}/merge-requests/{id}/reject.
// Mount this behind auth.RequireRole(auth.RoleAdmin, ...) — this handler
// itself does not check the caller's role.
func (h *Handlers) Reject(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, StatusRejected)
}

func (h *Handlers) setStatus(w http.ResponseWriter, r *http.Request, status Status) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	mr, err := h.Store.SetStatus(repo, id, status)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mr)
}

func (h *Handlers) repoExists(repo string) bool {
	repos, err := h.Repos.List()
	if err != nil {
		return false
	}
	return slices.Contains(repos, repo)
}

func (h *Handlers) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "404 merge request not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidRepo), errors.Is(err, ErrInvalidID), errors.Is(err, ErrInvalidStatus):
		http.Error(w, "400 Bad Request", http.StatusBadRequest)
	default:
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handlers) writeBranchError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBranchNotFound) {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
