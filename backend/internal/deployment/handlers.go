package deployment

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"sort"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// Handlers exposes the deploy request API as http.HandlerFuncs, meant to
// be mounted by internal/server behind auth.RequireAuth (and, for
// Approve/Reject, auth.RequireRole(auth.RoleAdmin, ...) — approving a
// deploy request is the one action in this package that actually changes
// what's running).
type Handlers struct {
	Store   *Store
	Repos   *repostore.Store
	Targets *Targets
	// Pipeline is optional in the same nil-safe spirit as Audit/Notify
	// elsewhere: without one, Create still works (a request can be
	// opened and reviewed) but Approve always fails clearly rather than
	// panicking — a repo can exist before deploy is wired up at all.
	Pipeline *deploy.Pipeline
	// CheckoutRoot is where Approve materializes a temporary source
	// checkout before building it (see deploy.Checkout). Removed again
	// once that deploy attempt finishes, success or failure.
	CheckoutRoot string

	Audit  *audit.Logger
	Notify *notify.Store
	Users  *users.Store
	// Access is optional; a nil Store means every caller sees every repo
	// (see internal/access). ListAll is the only place this package needs
	// it — see taskboard.Handlers.Access's doc comment for why.
	Access *access.Store
}

type createRequest struct {
	Environment  string `json:"environment"`
	SourceBranch string `json:"sourceBranch"`
}

// Create handles POST /api/repos/{repo}/deployments.
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
	if req.Environment == "" || req.SourceBranch == "" {
		http.Error(w, "400 environment and sourceBranch are required", http.StatusBadRequest)
		return
	}

	if _, err := h.Targets.Find(repo, req.Environment); err != nil {
		http.Error(w, "400 no deploy target configured for this repo and environment", http.StatusBadRequest)
		return
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	if !branchExists(gitRepo, req.SourceBranch) {
		http.Error(w, "400 sourceBranch not found", http.StatusBadRequest)
		return
	}

	created, err := h.Store.Create(repo, req.Environment, req.SourceBranch, user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log(user.Subject, audit.ActionDeploymentOpened, repo, created.ID,
		"Deploy isteği açıldı: "+repo+" → "+req.Environment+" ("+req.SourceBranch+")")
	h.notifyAdmins(repo, created)

	writeJSON(w, http.StatusCreated, created)
}

// Environments handles GET /api/repos/{repo}/deploy-targets, returning the
// environment names actually configured for repo — what the "new deploy
// request" form's environment picker offers, so it can only ever submit a
// value Approve will later find a Target for (barring a config change in
// between), rather than free text.
func (h *Handlers) Environments(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, h.Targets.Environments(repo))
}

// List handles GET /api/repos/{repo}/deployments.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	reqs, err := h.Store.List(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// Get handles GET /api/repos/{repo}/deployments/{id}.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	req, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ListAll handles GET /api/deployments — every repository's deploy
// requests in one response, newest first, optionally narrowed with
// ?status=. Backs the dashboard's "onay bekleyen deploy istekleri" view,
// the same cross-repo shape mergerequest.ListAll and taskboard.ListAll
// already established.
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

	all := []Request{}
	for _, repo := range repos {
		reqs, err := h.Store.List(repo)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, req := range reqs {
			if status != "" && req.Status != status {
				continue
			}
			all = append(all, req)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, all)
}

// Approve handles POST /api/repos/{repo}/deployments/{id}/approve. It
// actually runs the deploy (checkout → build → version → IIS swap, with
// secrets injected per the configured Target) before returning — this is
// a synchronous, potentially slow HTTP call by design, matching how
// Approve on a merge request also does its real work (the fast-forward)
// inline rather than queuing it. Mount this behind
// auth.RequireRole(auth.RoleAdmin, ...) — this handler itself does not
// check the caller's role.
//
// A missing deploy Target rejects the call outright (400) and leaves the
// request StatusPending — that's a configuration gap an admin can fix
// without needing a new request. Once a deploy is actually attempted,
// though, its outcome (success or failure) is recorded as a terminal
// status: Checkout/Build/IIS failures move the request to StatusFailed
// rather than leaving it retriable in place, because a real attempt did
// happen and its failure is exactly the "deploy sonucu" the design doc
// asks to notify about — retrying means opening a new request, the same
// way a failed CI run isn't silently re-run in place.
func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}
	if h.Pipeline == nil {
		http.Error(w, "500 deploy pipeline is not configured on this server", http.StatusInternalServerError)
		return
	}

	req, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}
	if req.Status != StatusPending {
		http.Error(w, "409 deploy request is not pending", http.StatusConflict)
		return
	}

	target, err := h.Targets.Find(repo, req.Environment)
	if err != nil {
		http.Error(w, "400 no deploy target configured for this repo and environment", http.StatusBadRequest)
		return
	}

	gitRepo, err := h.Repos.Open(repo)
	if err != nil {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	checkoutDir, err := os.MkdirTemp(h.CheckoutRoot, "checkout-*")
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(checkoutDir)

	if _, err := deploy.Checkout(gitRepo, req.SourceBranch, checkoutDir); err != nil {
		h.finishFailed(w, repo, id, req, err)
		return
	}

	releaseDir, err := h.Pipeline.Deploy(
		checkoutDir, target.Recipe, repo, req.Environment, target.SiteName, target.KeepVersions, target.SecretsTarget,
	)
	// ErrPruneFailed means the release is already live and only cleanup of
	// old releases failed afterward — that's a successful deploy from the
	// requester's point of view (see Pipeline.Deploy's own doc comment),
	// so it's treated as success here too rather than StatusFailed.
	if err != nil && !errors.Is(err, deploy.ErrPruneFailed) {
		h.finishFailed(w, repo, id, req, err)
		return
	}

	updated, storeErr := h.Store.Decide(repo, id, StatusDeployed, releaseDir, "")
	if storeErr != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log("system", audit.ActionDeploymentSuccess, repo, id,
		"Deploy edildi: "+repo+" → "+req.Environment+" ("+releaseDir+")")
	h.notifyAuthor(req, "Deploy başarılı: "+repo+" → "+req.Environment)

	writeJSON(w, http.StatusOK, updated)
}

// finishFailed records a deploy attempt that started but failed, and
// responds 200 with the now-StatusFailed request: the HTTP call itself
// succeeded (the approval was processed and its outcome recorded), the
// deploy did not.
func (h *Handlers) finishFailed(w http.ResponseWriter, repo, id string, req Request, deployErr error) {
	updated, storeErr := h.Store.Decide(repo, id, StatusFailed, "", deployErr.Error())
	if storeErr != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	_ = h.Audit.Log("system", audit.ActionDeploymentFailed, repo, id,
		"Deploy başarısız: "+repo+" → "+req.Environment+" ("+deployErr.Error()+")")
	h.notifyAuthor(req, "Deploy başarısız: "+repo+" → "+req.Environment+" — "+deployErr.Error())

	writeJSON(w, http.StatusOK, updated)
}

// Reject handles POST /api/repos/{repo}/deployments/{id}/reject. Mount
// this behind auth.RequireRole(auth.RoleAdmin, ...) — this handler itself
// does not check the caller's role.
func (h *Handlers) Reject(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	id := r.PathValue("id")
	if !h.repoExists(repo) {
		http.Error(w, "404 repository not found", http.StatusNotFound)
		return
	}

	req, err := h.Store.Get(repo, id)
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	updated, err := h.Store.Decide(repo, id, StatusRejected, "", "")
	if err != nil {
		h.writeStoreError(w, err)
		return
	}

	_ = h.Audit.Log("system", audit.ActionDeploymentReject, repo, id,
		"Deploy isteği reddedildi: "+repo+" → "+req.Environment)
	h.notifyAuthor(req, "Deploy isteğiniz reddedildi: "+repo+" → "+req.Environment)

	writeJSON(w, http.StatusOK, updated)
}

// notifyAdmins tells every Admin that a deploy request was just opened —
// it's "onay bekleyen talep", one of the design doc's named notification
// triggers. No-op unless both Notify and Users are wired, the same
// optionality mergerequest.notifyAdmins already established.
func (h *Handlers) notifyAdmins(repo string, req Request) {
	if h.Notify == nil || h.Users == nil {
		return
	}
	all, err := h.Users.List()
	if err != nil {
		return
	}
	message := "Yeni deploy isteği: " + repo + " → " + req.Environment + " (" + req.SourceBranch + ")"
	link := "/repos/" + repo + "/deployments/" + req.ID
	for _, u := range all {
		if u.Role != string(auth.RoleAdmin) {
			continue
		}
		_, _ = h.Notify.Create(u.Subject, "deployment_opened", message, link)
	}
}

// notifyAuthor tells req's author the outcome of their deploy request —
// "deploy sonucu" is named explicitly in the design doc as a notification
// trigger, unlike a merge request's approve/reject.
func (h *Handlers) notifyAuthor(req Request, message string) {
	if h.Notify == nil {
		return
	}
	link := "/repos/" + req.Repo + "/deployments/" + req.ID
	_, _ = h.Notify.Create(req.Author, "deployment_decided", message, link)
}

func branchExists(repo *git.Repository, branch string) bool {
	_, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	return err == nil
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
		http.Error(w, "404 deploy request not found", http.StatusNotFound)
	case errors.Is(err, ErrNotPending):
		http.Error(w, "409 deploy request is not pending", http.StatusConflict)
	case errors.Is(err, ErrInvalidRepo), errors.Is(err, ErrInvalidID):
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
