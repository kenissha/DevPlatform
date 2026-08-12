package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/deployment"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repoapi"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	// The "/git/" prefix here must stay in sync with gitserver.Prefix
	// ("/git") — see internal/gitserver.NewHandler's doc comment.
	mux.Handle("/git/", gitHandler)
	// /api/me returns the caller's identity and, as a side effect, records
	// them in the people registry (see internal/users) — that just-in-time
	// provisioning is what keeps the assignee picker's list of colleagues
	// accurate without anyone maintaining it by hand.
	mux.Handle("GET /api/me", authMiddleware(handleMe(deps.Users)))
	mux.Handle("GET /api/users", authMiddleware(handleUsers(deps.Users)))

	// Any authenticated user can list repos and branches; creating a repo
	// is an administrative action (project setup), so it's Admin-only.
	mux.Handle("GET /api/repos", authMiddleware(http.HandlerFunc(repos.List)))
	mux.Handle("POST /api/repos", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(repos.Create))))
	mux.Handle("GET /api/repos/{repo}/branches", authMiddleware(http.HandlerFunc(repos.Branches)))

	// Any authenticated user (Developer or Admin) can open a merge request
	// or read its diff; only an Admin can approve/reject it — "kör onay
	// yoktur" is enforced by Get always recomputing the diff live, and
	// approve/reject being gated separately from create/list.
	mux.Handle("POST /api/repos/{repo}/merge-requests", authMiddleware(http.HandlerFunc(mr.Create)))
	mux.Handle("GET /api/repos/{repo}/merge-requests", authMiddleware(http.HandlerFunc(mr.List)))
	mux.Handle("GET /api/repos/{repo}/merge-requests/{id}", authMiddleware(http.HandlerFunc(mr.Get)))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/approve",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(mr.Approve))))
	mux.Handle("POST /api/repos/{repo}/merge-requests/{id}/reject",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(mr.Reject))))

	// The task board has no Admin-only actions — see taskboard.Store.Update's
	// doc comment for why it's deliberately lighter-weight than merge
	// request review.
	mux.Handle("POST /api/repos/{repo}/tasks", authMiddleware(http.HandlerFunc(tasks.Create)))
	mux.Handle("GET /api/repos/{repo}/tasks", authMiddleware(http.HandlerFunc(tasks.List)))
	mux.Handle("GET /api/repos/{repo}/tasks/{id}", authMiddleware(http.HandlerFunc(tasks.Get)))
	mux.Handle("PATCH /api/repos/{repo}/tasks/{id}", authMiddleware(http.HandlerFunc(tasks.Update)))

	// Cross-repo views. These back the dashboard, which answers "who is
	// working on what" and "what is waiting on me" across every repository
	// at once — questions the per-repo endpoints above can't answer without
	// the client fanning out a request per repo.
	mux.Handle("GET /api/tasks", authMiddleware(http.HandlerFunc(tasks.ListAll)))
	mux.Handle("GET /api/merge-requests", authMiddleware(http.HandlerFunc(mr.ListAll)))

	// Repository insight: read-only, so not role-gated.
	mux.Handle("GET /api/repos/{repo}/commits", authMiddleware(http.HandlerFunc(stats.Commits)))
	mux.Handle("GET /api/repos/{repo}/contributors", authMiddleware(http.HandlerFunc(stats.Contributors)))
	mux.Handle("GET /api/repos/{repo}/activity", authMiddleware(http.HandlerFunc(stats.Activity)))

	// The audit log is readable by any authenticated user — see
	// audit.Handlers' doc comment for why it isn't Admin-gated.
	mux.Handle("GET /api/audit", authMiddleware(http.HandlerFunc(auditLog.List)))

	// Per-user notifications: every authenticated user can list and mark
	// read only their own — see notify.Handlers' doc comment.
	mux.Handle("GET /api/notifications", authMiddleware(http.HandlerFunc(notifications.List)))
	mux.Handle("POST /api/notifications/{id}/read", authMiddleware(http.HandlerFunc(notifications.MarkRead)))

	// Deploy requests: any authenticated user can open one or read its
	// status; only an Admin can approve (which actually runs the deploy)
	// or reject — the same review-then-act split merge requests already
	// use, see deployment.Handlers.Approve's doc comment.
	mux.Handle("GET /api/repos/{repo}/deploy-targets", authMiddleware(http.HandlerFunc(deployments.Environments)))
	mux.Handle("POST /api/repos/{repo}/deployments", authMiddleware(http.HandlerFunc(deployments.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", authMiddleware(http.HandlerFunc(deployments.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", authMiddleware(http.HandlerFunc(deployments.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.Approve))))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject",
		authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.Reject))))
	mux.Handle("GET /api/deployments", authMiddleware(http.HandlerFunc(deployments.ListAll)))

	return mux
}

func handleMe(registry *users.Store) http.Handler {
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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(user); err != nil {
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
