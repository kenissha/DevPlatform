package gitstats

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-git/go-git/v6"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/gitemails"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

// Handlers exposes repository insight over HTTP, meant to be mounted by
// internal/server behind auth.RequireAuth. Everything here is read-only,
// so no endpoint is role-gated: seeing who is working on what is the point
// of the platform, not a privilege.
type Handlers struct {
	Repos *repostore.Store
	// GitEmails supplies the extra author addresses a person commits
	// under, on top of their platform email (see internal/gitemails for
	// why one address is rarely enough). Optional: a nil Store means
	// nobody registered any, so only the platform email is matched.
	GitEmails *gitemails.Store
	// Access narrows Contributions to the repos its caller may see.
	// Optional in the same sense as elsewhere: a nil Store means nobody
	// is restricted (see internal/access). The per-repo endpoints below
	// don't need it — internal/server already wraps each of them in
	// access.RequireRepoAccess, which has a {repo} to check. Contributions
	// spans every repo at once, so it has to filter for itself, the same
	// way mergerequest.Handlers.ListAll does.
	Access *access.Store
}

const (
	defaultCommitLimit  = 20
	maxCommitLimit      = 200
	defaultActivityDays = 30
	maxActivityDays     = 365
	// A full year, matching the heatmap the panel draws from it.
	defaultContributionDays = 365
)

// Commits handles GET /api/repos/{repo}/commits?limit=N.
func (h *Handlers) Commits(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.open(w, r)
	if !ok {
		return
	}

	limit := intParam(r, "limit", defaultCommitLimit, 1, maxCommitLimit)
	commits, err := Commits(repo, limit)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

// BranchCommits handles
// GET /api/repos/{repo}/branch-commits?branch=X&base=main&limit=N — the
// branch detail page's commit list (see CommitsAhead's doc comment for
// the exact comparison it shows). base defaults to "main", the only
// branch DevPlatform ever protects.
//
// branch is a query parameter, not a {branch} path segment, on purpose:
// branch names can contain "/" (e.g. "feature/hakem-raporlari"), and
// IIS's request filtering rejects an encoded "%2F" in the URL PATH by
// default ("Double Escape Sequence", 404 before the request ever
// reaches this process) — confirmed live (2026-09-04) against the real
// deployment. A query value isn't subject to that same path-based
// filtering, so this sidesteps the problem instead of needing an IIS
// config change (which would trade away a real security check).
func (h *Handlers) BranchCommits(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.open(w, r)
	if !ok {
		return
	}

	branch := r.URL.Query().Get("branch")
	if branch == "" {
		http.Error(w, "400 branch query parameter is required", http.StatusBadRequest)
		return
	}
	base := r.URL.Query().Get("base")
	if base == "" {
		base = "main"
	}
	limit := intParam(r, "limit", defaultCommitLimit, 1, maxCommitLimit)

	commits, err := CommitsAhead(repo, branch, base, limit)
	if err != nil {
		if errors.Is(err, ErrBranchNotFound) {
			http.Error(w, "404 "+err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

// Contributors handles GET /api/repos/{repo}/contributors.
func (h *Handlers) Contributors(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.open(w, r)
	if !ok {
		return
	}

	contributors, err := Contributors(repo)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, contributors)
}

// Activity handles GET /api/repos/{repo}/activity?days=N.
func (h *Handlers) Activity(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.open(w, r)
	if !ok {
		return
	}

	days := intParam(r, "days", defaultActivityDays, 1, maxActivityDays)
	activity, err := Activity(repo, days)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, activity)
}

// contributionsResponse is the panel's contribution heatmap: one entry
// per day over the requested window (oldest first, gaps included) plus
// the window's total, so the header line doesn't have to re-sum it.
type contributionsResponse struct {
	Days  []DayCount `json:"days"`
	Total int        `json:"total"`
}

// Contributions handles GET /api/contributions?days=N — the calling
// user's own commits per day, across every repository they can see.
//
// Always scoped to the caller: there is no ?subject= parameter, so this
// can't be turned into "show me someone else's activity" by editing a
// URL. Commits are matched against the caller's platform email plus any
// address they registered (see ActivityByAuthors and internal/gitemails).
func (h *Handlers) Contributions(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	days := intParam(r, "days", defaultContributionDays, 1, maxActivityDays)

	// The platform email always counts, without needing to be
	// registered — it's the one address the platform actually knows is
	// theirs, and requiring people to re-enter it would be busywork.
	emails := []string{user.Email}
	registered, err := h.GitEmails.List(user.Subject)
	if err != nil {
		// Degrade to the platform email rather than failing: a graph
		// missing some commits still beats an error where a graph
		// should be.
		log.Printf("gitstats: failed to read registered git emails for %q: %v", user.Subject, err)
	}
	emails = append(emails, registered...)

	names, err := h.Repos.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	if user.Role != auth.RoleAdmin {
		names, err = h.Access.FilterRepos(user.Subject, names)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	// Merge the raw per-day maps first, then fill the range once — the
	// window's gaps are the same for every repo, so filling per repo and
	// summing afterwards would do the same work N times.
	merged := map[string]int{}
	for _, name := range names {
		repo, err := h.Repos.Open(name)
		if err != nil {
			// A repo that vanished between List and Open (or is somehow
			// unreadable) shouldn't take the whole dashboard down with
			// it — skip it and keep counting the rest.
			log.Printf("gitstats: skipping %q while counting contributions: %v", name, err)
			continue
		}
		counts, err := ActivityByAuthors(repo, emails, days)
		if err != nil {
			log.Printf("gitstats: skipping %q while counting contributions: %v", name, err)
			continue
		}
		for day, n := range counts {
			merged[day] += n
		}
	}

	filled := fillDays(merged, days)
	total := 0
	for _, d := range filled {
		total += d.Commits
	}
	writeJSON(w, http.StatusOK, contributionsResponse{Days: filled, Total: total})
}

func (h *Handlers) open(w http.ResponseWriter, r *http.Request) (*git.Repository, bool) {
	name := r.PathValue("repo")
	repo, err := h.Repos.Open(name)
	if err != nil {
		if errors.Is(err, repostore.ErrNotExist) || errors.Is(err, repostore.ErrInvalidName) {
			http.Error(w, "404 repository not found", http.StatusNotFound)
			return nil, false
		}
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}
	return repo, true
}

// intParam reads a bounded integer query parameter, falling back to def
// when it is absent or unparseable. Clamping rather than erroring keeps a
// hand-typed URL from turning into a 400 the UI would have to handle, and
// caps how much history one request can ask the server to walk.
func intParam(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
