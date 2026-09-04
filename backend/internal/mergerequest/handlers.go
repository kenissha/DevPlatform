package mergerequest

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// Handlers exposes the merge request API as http.HandlerFuncs, meant to be
// mounted by internal/server behind auth.RequireAuth (and, for
// Approve/Reject, auth.RequireRole(auth.RoleAdmin, ...)).
type Handlers struct {
	Store *Store
	Repos *repostore.Store
	// Audit is optional; a nil Logger records nothing (see internal/audit).
	Audit *audit.Logger
	// Notify is optional; a nil Store creates no notifications (see
	// internal/notify). Unlike Logger, notify.Store is not itself
	// nil-receiver-safe, so call sites check h.Notify != nil before use.
	Notify *notify.Store
	// Users resolves who the Admins are so a newly opened merge request
	// can notify all of them. Optional in the same sense as Notify: a nil
	// Users (or nil Notify) simply means no merge-request-opened
	// notifications are created, rather than a panic — a repo can be
	// used without notifications wired at all, same as without Audit.
	Users *users.Store
	// Access is optional; a nil Store means every caller sees every repo
	// (see internal/access). ListAll is the only place this package needs
	// it — see taskboard.Handlers.Access's doc comment for why.
	Access *access.Store
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
	// TargetBranch is allowed not to exist yet — a brand new repo's
	// default branch has no commits until an Admin pushes its first one
	// directly (protected refs reject everyone else's direct push, but
	// not an Admin's — see gitserver.WithAdmin/IsAdmin). Requesting a
	// review before that first push is still meaningful: the diff just
	// shows every file in SourceBranch as newly added (see Diff's doc
	// comment).
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
		"İnceleme isteği açıldı: "+mr.Title+" ("+mr.SourceBranch+" → "+mr.TargetBranch+")")

	h.notifyAdmins(repo, mr)

	writeJSON(w, http.StatusCreated, mr)
}

// notifyAdmins tells every Admin that mr was just opened. It is a no-op
// unless both Notify and Users are wired (see their doc comments on
// Handlers) — a merge request still opens successfully either way, since
// notification is a side-effect of opening, not a precondition for it.
func (h *Handlers) notifyAdmins(repo string, mr MergeRequest) {
	if h.Notify == nil || h.Users == nil {
		return
	}

	all, err := h.Users.List()
	if err != nil {
		return
	}

	message := "Yeni inceleme isteği açıldı: " + mr.Title + " (" + mr.SourceBranch + " → " + mr.TargetBranch + ") - " + repo
	link := "/repos/" + repo + "/merge-requests/" + mr.ID
	for _, u := range all {
		// users.User.Role is a bare string (set straight from the JWT's
		// "role" claim by users.Store.Upsert), not auth.Role, so it's
		// compared against the string form of auth.RoleAdmin rather than
		// auth.Role itself — this package already imports auth, so its
		// constant is used here instead of a bare "admin" literal.
		if u.Role != string(auth.RoleAdmin) {
			continue
		}
		_, _ = h.Notify.Create(u.Subject, "merge_request_opened", message, link)
	}
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
	if user, ok := auth.UserFromContext(r.Context()); ok && user.Role != auth.RoleAdmin {
		repos, err = h.Access.FilterRepos(user.Subject, repos)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
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

type decisionRequest struct {
	Note string `json:"note"`
}

// Approve handles POST /api/repos/{repo}/merge-requests/{id}/approve. It
// performs no git operation of any kind — it only records that a
// Yönetici reviewed this request and signed off, with an optional Note.
// Approving through the panel is not how TargetBranch (in practice
// always "main") actually advances: an Admin pushes the real, reviewed
// result there directly (gitserver's branch protection allows exactly
// that, and only that, for an Admin — see gitserver.WithAdmin/IsAdmin),
// using real git for any conflict resolution needed, unconstrained by
// whatever this project's pinned go-git version can or can't do. By the
// time this handler runs, that push has already happened; clicking
// Approve is the Yönetici's own record that they did it, not a request
// for this handler to do it for them. Mount this behind
// auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does not
// check the caller's role.
func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	var req decisionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	updated, err := h.Store.SetStatus(repo, id, StatusApproved, req.Note)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionMRApproved, repo, updated.ID,
			"İnceleme isteği onaylandı: "+updated.Title+" → "+updated.TargetBranch)
	}

	writeJSON(w, http.StatusOK, updated)
}

// Reject handles POST /api/repos/{repo}/merge-requests/{id}/reject —
// "henüz hazır değil, devam et": the Yönetici sends the request back
// with an optional Note explaining what's missing. There's no "re-open";
// the Geliştirici keeps working on the same source branch and opens a
// new request once it's addressed (Create has no branch-pair uniqueness
// constraint, so this is always available). Mount this behind
// auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does not
// check the caller's role.
func (h *Handlers) Reject(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	var req decisionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	mr, err := h.Store.SetStatus(repo, id, StatusRejected, req.Note)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok {
		_ = h.Audit.Log(user.Subject, audit.ActionMRRejected, repo, mr.ID, "İnceleme isteği reddedildi: "+mr.Title)
	}

	writeJSON(w, http.StatusOK, mr)
}

type branchPreview struct {
	Diff DiffResult `json:"diff"`
	// OpenRequest is this branch's currently open İnceleme İsteği
	// targeting the same target, if any — the branch page shows its
	// status instead of letting a developer open a duplicate.
	OpenRequest *MergeRequest `json:"openRequest,omitempty"`
	// LastRejected is the most recently rejected request for this exact
	// (branch, target) pair, only populated when there's no OpenRequest
	// — so the branch page can surface why, via its Note, without the
	// developer digging through the full İnceleme İstekleri list.
	LastRejected *MergeRequest `json:"lastRejected,omitempty"`
}

// BranchPreview handles
// GET /api/repos/{repo}/branch-preview?branch=X&target=main — the
// branch detail page's data source. target defaults to "main", the only
// branch DevPlatform ever protects.
//
// branch is a query parameter, not a {branch} path segment — see
// gitstats.Handlers.BranchCommits' doc comment for why (IIS's request
// filtering rejects an encoded "/" in the URL path by default;
// confirmed live 2026-09-04).
func (h *Handlers) BranchPreview(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		http.Error(w, "400 branch query parameter is required", http.StatusBadRequest)
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		target = "main"
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	diff, err := Diff(gitRepo, target, branch)
	if err != nil {
		h.writeBranchError(w, err)
		return
	}

	all, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	preview := branchPreview{Diff: diff}
	// Store.List returns newest first, so the first match of each kind
	// found here is already the most recent one.
	for _, candidate := range all {
		if candidate.SourceBranch != branch || candidate.TargetBranch != target {
			continue
		}
		mr := candidate
		if mr.Status == StatusOpen && preview.OpenRequest == nil {
			preview.OpenRequest = &mr
		}
		if mr.Status == StatusRejected && preview.LastRejected == nil {
			preview.LastRejected = &mr
		}
	}
	if preview.OpenRequest != nil {
		preview.LastRejected = nil
	}

	writeJSON(w, http.StatusOK, preview)
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
