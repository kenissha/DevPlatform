package mergerequest

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes the merge request API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth (and, for
// Approve/Reject, auth.RequireRole(auth.RoleAdmin, ...)).
type Handlers struct {
	Store *Store
	Repos *repostore.Store
	// Audit is optional; a nil Logger records nothing (see internal/audit).
	Audit *audit.Logger
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
	// TargetBranch is allowed not to exist yet — FastForwardMerge creates
	// it on approval (see its doc comment). This is the only way a brand
	// new repo's default branch ever gets a first commit, since direct
	// pushes to it are rejected unconditionally.
	if _, err := resolveBranchTip(gitRepo, req.TargetBranch); err != nil && !errors.Is(err, ErrBranchNotFound) {
		h.writeBranchError(w, err)
		return
	}

	mr, err := h.Store.Create(repo, req.Title, req.SourceBranch, req.TargetBranch, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log(user.Subject, audit.ActionMROpened, repo, mr.ID,
		"Merge isteği açıldı: "+mr.Title+" ("+mr.SourceBranch+" → "+mr.TargetBranch+")")

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

// ListAll handles GET /api/merge-requests — every repository's merge
// requests in one response, newest first, optionally narrowed with
// ?status=. Deliberately excludes the diff: this feeds the dashboard's
// "bekleyen incelemeler" list, and computing a diff per merge request
// across every repo would make an overview page pay for review-screen
// work nobody asked for yet.
func (h *Handlers) ListAll(w http.ResponseWriter, r *http.Request) {
	repos, err := h.Repos.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	status := Status(r.URL.Query().Get("status"))

	all := []MergeRequest{}
	for _, repo := range repos {
		mrs, err := h.Store.List(repo)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, mr := range mrs {
			if status != "" && mr.Status != status {
				continue
			}
			all = append(all, mr)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, all)
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

// Approve handles POST /api/repos/{repo}/merge-requests/{id}/approve. It
// actually performs the merge (fast-forwarding TargetBranch to
// SourceBranch's tip — see FastForwardMerge) before recording the
// approval; if the merge can't be done, the request is left StatusOpen
// rather than being marked approved without anything having actually
// merged. Mount this behind auth.RequireRole(auth.RoleAdmin, ...) — this
// handler itself does not check the caller's role.
func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
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
	if mr.Status != StatusOpen {
		http.Error(w, "409 merge request is not open", http.StatusConflict)
		return
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	mergedHash, err := FastForwardMerge(gitRepo, mr.TargetBranch, mr.SourceBranch)
	if err != nil {
		if errors.Is(err, ErrNotFastForward) {
			http.Error(w, "409 "+err.Error(), http.StatusConflict)
			return
		}
		h.writeBranchError(w, err)
		return
	}

	updated, err := h.Store.MarkApproved(repo, id, mergedHash.String())
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionMRApproved, repo, updated.ID,
			"Merge onaylandı ve birleştirildi: "+updated.Title+" → "+updated.TargetBranch+
				" ("+mergedHash.String()[:8]+")")
	}

	writeJSON(w, http.StatusOK, updated)
}

// Reject handles POST /api/repos/{repo}/merge-requests/{id}/reject.
// Mount this behind auth.RequireRole(auth.RoleAdmin, ...) — this handler
// itself does not check the caller's role.
func (h *Handlers) Reject(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	mr, err := h.Store.SetStatus(repo, id, StatusRejected)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionMRRejected, repo, mr.ID, "Merge reddedildi: "+mr.Title)
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
	case errors.Is(err, ErrNotOpen):
		http.Error(w, "409 merge request is not open", http.StatusConflict)
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
