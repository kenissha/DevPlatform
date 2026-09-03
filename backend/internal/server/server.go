package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/deployment"
	"github.com/kenissha/DevPlatform/backend/internal/displaynames"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/gittoken"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repoapi"
	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
	"github.com/kenissha/DevPlatform/backend/internal/taskboard"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// Deps are the collaborators NewRouter mounts. A struct rather than a
// parameter list: this grew a new argument with almost every feature, and
// each time every call site had to be edited even though nothing about
// them changed. Adding a field here leaves existing construction sites
// compiling untouched.
type Deps struct {
	// GitHandler serves the git smart-HTTP protocol under its own prefix
	// (see internal/gitserver).
	GitHandler http.Handler
	// AuthMiddleware wraps routes requiring a valid JWT (see internal/auth).
	// /healthz and the git routes are deliberately outside it.
	AuthMiddleware func(http.Handler) http.Handler

	MergeRequests *mergerequest.Handlers // merge request review API
	Repos         *repoapi.Handlers      // repository listing/creation/branches
	Tasks         *taskboard.Handlers    // task board
	Stats         *gitstats.Handlers     // read-only repository insight
	Audit         *audit.Handlers        // recorded action history
	Notifications *notify.Handlers       // per-user notifications
	Deployments   *deployment.Handlers   // onay-triggered build+deploy
	// Users is the people registry. Optional: when nil, /api/users returns
	// an empty list and no just-in-time provisioning happens on /api/me.
	Users *users.Store
	// Access controls per-person repository visibility (see internal/access,
	// Faz 3's "proje bazlı yetkilendirme"). Optional: a nil Store means
	// nobody is restricted, matching the platform's behavior before this
	// existed. When set, every repo-scoped route below additionally
	// requires access.RequireRepoAccess to pass, and /api/access exposes
	// the admin-only management API.
	Access *access.Store
	// DisplayNames overrides how a subject's name appears in the panel
	// (see internal/displaynames — the SSO JWT carries no name claim, only
	// subject/email). Optional: a nil Store means everyone falls back to
	// their email, matching today's behavior.
	DisplayNames *displaynames.Store
	// SecretsVault stores per-(repo, environment) encrypted secrets an
	// admin has uploaded from the panel (see internal/secretsvault) —
	// deploy.Pipeline reads from it internally when a deploy target's
	// secretsTarget is set. Optional: a nil Store means /api/secrets
	// responds 503 rather than accepting an upload nothing will ever
	// consume, matching Store.Get's own nil-vault-not-configured posture
	// deploy.Pipeline already relies on.
	SecretsVault *secretsvault.Store
	// GitTokens issues and revokes the per-person git credentials that
	// gate the /git/ route (see internal/gittoken). Not optional in
	// practice — cmd/devplatform/main.go always constructs one — but a
	// nil GitTokens wouldn't fail at startup (a method value on a nil
	// pointer receiver binds fine); it would panic on the first request
	// to either route below. Nil-checking it here wouldn't change that,
	// so it's left to a caller to wire correctly.
	GitTokens *gittoken.Handlers
}

// NewRouter builds the top-level HTTP router.
func NewRouter(deps Deps) *http.ServeMux {
	gitHandler := deps.GitHandler
	authMiddleware := deps.AuthMiddleware
	mr := deps.MergeRequests
	repos := deps.Repos
	tasks := deps.Tasks
	stats := deps.Stats
	auditLog := deps.Audit
	notifications := deps.Notifications
	deployments := deps.Deployments
	accessHandlers := &access.Handlers{Store: deps.Access}
	displayNameHandlers := &displaynames.Handlers{Store: deps.DisplayNames}
	secretsHandlers := &secretsvault.Handlers{Store: deps.SecretsVault}
	gitTokens := deps.GitTokens

	// repoScoped wraps a handler for any route with a {repo} path value:
	// authentication first, then access.RequireRepoAccess (a nil
	// deps.Access means nobody is restricted, so this is a no-op until an
	// admin actually configures one — see access.Store's doc comment).
	repoScoped := func(h http.Handler) http.Handler {
		return authMiddleware(access.RequireRepoAccess(deps.Access, h))
	}
	// repoScopedAdmin is repoScoped plus an Admin-only check, for
	// repo-scoped actions that were already Admin-gated (approving a merge
	// request or deploy) — project visibility is checked before role,
	// so a developer restricted away from a repo gets the same 403 an
	// outsider would, rather than a role-based one revealing the repo's
	// admin-only actions exist at all.
	repoScopedAdmin := func(h http.Handler) http.Handler {
		return authMiddleware(access.RequireRepoAccess(deps.Access, auth.RequireRole(auth.RoleAdmin, h)))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	// The "/git/" prefix here must stay in sync with gitserver.Prefix
	// ("/git") — see internal/gitserver.NewHandler's doc comment.
	mux.Handle("/git/", gitHandler)
	// /api/me returns the caller's identity and, as a side effect, records
	// them in the people registry (see internal/users) — that just-in-time
	// provisioning is what keeps the assignee picker's list of colleagues
	// accurate without anyone maintaining it by hand.
	mux.Handle("GET /api/me", authMiddleware(handleMe(deps.Users, deps.DisplayNames)))
	mux.Handle("GET /api/users", authMiddleware(handleUsers(deps.Users)))

	// Any authenticated user can list repos (List narrows the result to
	// what they're allowed to see itself — no {repo} in this path for
	// repoScoped to check); creating a repo is an administrative action
	// (project setup), so it's Admin-only. Branches is repo-scoped.
	mux.Handle("GET /api/repos", authMiddleware(http.HandlerFunc(repos.List)))
	mux.Handle("POST /api/repos", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(repos.Create))))
	mux.Handle("GET /api/repos/{repo}/branches", repoScoped(http.HandlerFunc(repos.Branches)))

	// Any authenticated user (Developer or Admin) can open a merge request
	// or read its diff; only an Admin can approve/reject it — "kör onay
	// yoktur" is enforced by Get always recomputing the diff live, and
	// approve/reject being gated separately from create/list.
	mux.Handle("POST /api/repos/{repo}/merge-requests", repoScoped(http.HandlerFunc(mr.Create)))
	mux.Handle("GET /api/repos/{repo}/merge-requests", repoScoped(http.HandlerFunc(mr.List)))
	mux.Handle("GET /api/repos/{repo}/merge-requests/{id}", repoScoped(http.HandlerFunc(mr.Get)))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/approve", repoScopedAdmin(http.HandlerFunc(mr.Approve)))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/reject", repoScopedAdmin(http.HandlerFunc(mr.Reject)))

	// The task board has no Admin-only actions — see taskboard.Store.Update's
	// doc comment for why it's deliberately lighter-weight than merge
	// request review.
	mux.Handle("POST /api/repos/{repo}/tasks", repoScoped(http.HandlerFunc(tasks.Create)))
	mux.Handle("GET /api/repos/{repo}/tasks", repoScoped(http.HandlerFunc(tasks.List)))
	mux.Handle("GET /api/repos/{repo}/tasks/{id}", repoScoped(http.HandlerFunc(tasks.Get)))
	mux.Handle("PATCH /api/repos/{repo}/tasks/{id}", repoScoped(http.HandlerFunc(tasks.Update)))

	// Cross-repo views. These back the dashboard, which answers "who is
	// working on what" and "what is waiting on me" across every repository
	// at once — questions the per-repo endpoints above can't answer without
	// the client fanning out a request per repo. No {repo} in these paths
	// for repoScoped to check; each handler narrows its own result via its
	// Access field instead (see taskboard.Handlers.Access's doc comment).
	mux.Handle("GET /api/tasks", authMiddleware(http.HandlerFunc(tasks.ListAll)))
	mux.Handle("GET /api/merge-requests", authMiddleware(http.HandlerFunc(mr.ListAll)))

	// Repository insight: read-only, so not role-gated (still repo-scoped).
	mux.Handle("GET /api/repos/{repo}/commits", repoScoped(http.HandlerFunc(stats.Commits)))
	mux.Handle("GET /api/repos/{repo}/contributors", repoScoped(http.HandlerFunc(stats.Contributors)))
	mux.Handle("GET /api/repos/{repo}/activity", repoScoped(http.HandlerFunc(stats.Activity)))

	// The audit log is readable by any authenticated user — see
	// audit.Handlers' doc comment for why it isn't Admin-gated. Not
	// repo-scoped: it spans every repo already, same as the /api/tasks-style
	// aggregate views above.
	mux.Handle("GET /api/audit", authMiddleware(http.HandlerFunc(auditLog.List)))

	// Per-user notifications: every authenticated user can list and mark
	// read only their own — see notify.Handlers' doc comment.
	mux.Handle("GET /api/notifications", authMiddleware(http.HandlerFunc(notifications.List)))
	mux.Handle("POST /api/notifications/{id}/read", authMiddleware(http.HandlerFunc(notifications.MarkRead)))

	// Deploy requests: any authenticated user can open one or read its
	// status; only an Admin can approve (which actually runs the deploy)
	// or reject — the same review-then-act split merge requests already
	// use, see deployment.Handlers.Approve's doc comment.
	mux.Handle("GET /api/repos/{repo}/deploy-targets", repoScoped(http.HandlerFunc(deployments.Environments)))
	mux.Handle("POST /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", repoScoped(http.HandlerFunc(deployments.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve", repoScopedAdmin(http.HandlerFunc(deployments.Approve)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject", repoScopedAdmin(http.HandlerFunc(deployments.Reject)))
	mux.Handle("GET /api/deployments", authMiddleware(http.HandlerFunc(deployments.ListAll)))

	// Rollback: Admin-only like Approve/Reject, but with no pending/review
	// stage of its own — see deployment.Handlers.Rollback's doc comment
	// for why an already-live release doesn't need a second approval step.
	mux.Handle("GET /api/repos/{repo}/deployments/{environment}/releases", repoScopedAdmin(http.HandlerFunc(deployments.Releases)))
	mux.Handle("POST /api/repos/{repo}/deployments/{environment}/rollback", repoScopedAdmin(http.HandlerFunc(deployments.Rollback)))

	// Deploy-target management: entirely Admin-only, not repo-scoped —
	// unlike a deploy request, a target isn't attached to one already-visible
	// repo the caller is proven to see first, so this follows /api/access's
	// pattern (auth.RequireRole directly) rather than repoScopedAdmin's.
	// GET /api/allowed-sites is the read-only, ops-managed site list this
	// API validates siteName against but can never write to — see
	// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md.
	mux.Handle("GET /api/deploy-targets", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.ListTargets))))
	mux.Handle("PUT /api/deploy-targets/{repo}/{environment}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.SetTarget))))
	mux.Handle("DELETE /api/deploy-targets/{repo}/{environment}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.DeleteTarget))))
	mux.Handle("GET /api/allowed-sites", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.ListAllowedSites))))

	// Per-project authorization management: entirely Admin-only, not
	// repo-scoped by access.RequireRepoAccess itself (that middleware
	// checks whether the caller can see one {repo}; this is the API that
	// decides that for everyone else, so it can't be gated by its own
	// output).
	mux.Handle("GET /api/access", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.List))))
	mux.Handle("PUT /api/access/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.Set))))
	mux.Handle("DELETE /api/access/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.Clear))))

	mux.Handle("GET /api/display-names", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.List))))
	mux.Handle("PUT /api/display-names/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.Set))))
	mux.Handle("DELETE /api/display-names/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.Clear))))
	// Write-only by design — see secretsvault.Handlers's doc comment for
	// why there is no corresponding GET.
	mux.Handle("PUT /api/secrets/{repo}/{environment}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(secretsHandlers.Set))))

	// Per-person git credentials (see internal/gittoken). Anyone can mint
	// their own (no {subject} in the path — it always targets the
	// caller's own JWT subject); only an Admin can revoke someone else's,
	// the same split /api/access uses between "sees own" and "manages
	// everyone".
	mux.Handle("POST /api/me/git-token", authMiddleware(http.HandlerFunc(gitTokens.GenerateMine)))
	mux.Handle("DELETE /api/git-token/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(gitTokens.Revoke))))
	mux.Handle("GET /api/me/git-tokens", authMiddleware(http.HandlerFunc(gitTokens.ListMine)))
	mux.Handle("DELETE /api/me/git-tokens/{id}", authMiddleware(http.HandlerFunc(gitTokens.RevokeMine)))

	return mux
}

// meResponse is /api/me's response shape: the authenticated identity plus
// a display name resolved via internal/displaynames — kept as a local
// wrapper rather than a field on auth.User itself, since auth.User is used
// internally for authorization decisions that have nothing to do with how
// a name is displayed.
type meResponse struct {
	Subject     string    `json:"subject"`
	Email       string    `json:"email"`
	Role        auth.Role `json:"role"`
	DisplayName string    `json:"displayName"`
}

func handleMe(registry *users.Store, displayNames *displaynames.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Just-in-time provisioning. A registry write failure must not fail
		// the request: the caller is authenticated either way, and losing
		// their entry costs an assignee-picker row, not access.
		if registry != nil {
			if _, err := registry.Upsert(user.Subject, user.Email, string(user.Role)); err != nil {
				log.Printf("handleMe: failed to record user %q: %v", user.Subject, err)
			}
		}

		resp := meResponse{
			Subject:     user.Subject,
			Email:       user.Email,
			Role:        user.Role,
			DisplayName: displayNames.Get(user.Subject, user.Email),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("handleMe: failed to encode response: %v", err)
		}
	})
}

func handleUsers(registry *users.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list := []users.User{}
		if registry != nil {
			var err error
			list, err = registry.List()
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(list); err != nil {
			log.Printf("handleUsers: failed to encode response: %v", err)
		}
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handleHealth: failed to encode response: %v", err)
	}
}
