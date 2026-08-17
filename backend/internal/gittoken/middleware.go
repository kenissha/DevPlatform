package gittoken

import (
	"net/http"
	"path"
	"regexp"
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

		admin, err := isAdmin(usersStore, subject)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !admin {
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

var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// repoNameFromPath extracts "foo" from a git smart-HTTP request path like
// "/git/foo.git/info/refs" (gitserver.Prefix + "/" + name + ".git" +
// anything). It parses by path segment, not by searching for ".git"
// anywhere in the string: only the FIRST segment is ever treated as the
// repo name, it must exactly equal "<name>.git" with name matching
// repostore's own naming rule, and everything after it must already be a
// clean relative path (no ".." anywhere). This agrees with go-git's own
// resolution by construction rather than by enumerating attack shapes —
// an earlier version that only rejected a second literal ".git" in the
// remainder was proven bypassable, because gitserver.NewHandler's loader
// runs strict=false and auto-appends ".git" during resolution, so a
// target repo need not appear with a ".git" suffix in the request path
// at all (e.g. "/git/allowed.git/../secret/info/refs" resolves to the
// on-disk "secret.git" even though "secret" never appears with ".git" in
// the request). Requiring the remainder to already be ".."-free closes
// that and every other shape in the same class, since any successful
// escape into a sibling repo's directory necessarily routes through a
// ".." component somewhere in the remainder.
func repoNameFromPath(urlPath string) (repo string, ok bool) {
	rest, ok := strings.CutPrefix(urlPath, gitserver.Prefix+"/")
	if !ok {
		return "", false
	}
	segment, remainder, _ := strings.Cut(rest, "/")
	name, hasSuffix := strings.CutSuffix(segment, ".git")
	if !hasSuffix || name == "" || !validRepoName.MatchString(name) {
		return "", false
	}
	if remainder != "" && path.Clean("/"+remainder) != "/"+remainder {
		return "", false
	}
	return name, true
}

func isAdmin(usersStore *users.Store, subject string) (bool, error) {
	u, ok, err := usersStore.Get(subject)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return u.Role == string(auth.RoleAdmin), nil
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform Git"`)
	http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
}
