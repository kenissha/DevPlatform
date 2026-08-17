package gittoken

import (
	"net/http"
	"strings"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// RequireTokenAndAccess wraps next (the git smart-HTTP handler) with:
// authentication against tokens (HTTP Basic Auth, username = subject,
// password = raw token — what a `git` client sends), then the exact same
// per-repo authorization the panel API already uses
// (access.Store.CanAccess), extracting the repo name from the request
// path. Admins bypass the repo check, mirroring
// access.RequireRepoAccess's own admin-bypass rule — but since a git
// Basic Auth request carries no role claim (unlike a panel JWT), the
// role has to be looked up in usersStore instead. A subject who has
// never used the panel (so has no usersStore entry yet) is simply
// treated as non-admin here; that only matters if they're also
// repo-restricted, since an unrestricted subject passes the access check
// either way.
func RequireTokenAndAccess(tokens *Store, accessStore *access.Store, usersStore *users.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, token, ok := r.BasicAuth()
		if !ok || !tokens.Verify(subject, token) {
			unauthorized(w)
			return
		}

		repo, ok := repoNameFromPath(r.URL.Path)
		if !ok {
			http.Error(w, "400 Bad Request", http.StatusBadRequest)
			return
		}

		if !isAdmin(usersStore, subject) {
			allowed, err := accessStore.CanAccess(subject, repo)
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, "403 Forbidden", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// repoNameFromPath extracts "foo" from a git smart-HTTP request path like
// "/git/foo.git/info/refs" (gitserver.Prefix + "/" + name + ".git" +
// anything). Repo names are restricted elsewhere (repostore) to
// [a-zA-Z0-9_-]+, so the first ".git" occurrence is always the real
// boundary — no path-traversal concern, since the result is only ever
// used as a comparison key into access.Store, never as a filesystem path.
func repoNameFromPath(path string) (repo string, ok bool) {
	rest, ok := strings.CutPrefix(path, gitserver.Prefix+"/")
	if !ok {
		return "", false
	}
	repo, _, found := strings.Cut(rest, ".git")
	if !found || repo == "" {
		return "", false
	}
	return repo, true
}

func isAdmin(usersStore *users.Store, subject string) bool {
	u, ok, err := usersStore.Get(subject)
	if err != nil || !ok {
		return false
	}
	return u.Role == string(auth.RoleAdmin)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform Git"`)
	http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
}
