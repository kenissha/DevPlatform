package gittoken

import (
	"net/http"
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
// anything), and validates it before returning it.
//
// Two checks are load-bearing here, not decorative:
//
//  1. The naming-pattern check (^[a-zA-Z0-9_-]+$, matching repostore's own
//     rule at repo-creation time) rejects garbage/injection in the
//     extracted substring.
//
//  2. The "second .git" check rejects any path where another ".git"
//     appears after the first one. This is the one that actually closes
//     the path-traversal bypass a security review caught: this function
//     cuts at the FIRST ".git" (so "/git/allowed.git/../secret.git/info/refs"
//     extracts "allowed" — a perfectly valid, allow-listed name, which the
//     naming-pattern check alone would happily pass), while go-git's own
//     backend (backend/http.go) parses the same request path independently
//     and cuts at the LAST service-suffix segment, so after go-billy's
//     Chroot cleans "allowed.git/../secret.git" down to "secret.git" (which
//     stays inside the chroot root, so the boundary check doesn't reject
//     it), go-git actually resolves and serves "secret". A regex on
//     character class can never catch that divergence, because the
//     attacker's chosen prefix ("allowed") is by construction a
//     clean, valid-shaped name — the mismatch is structural (first-cut vs.
//     last-cut disagreement), not lexical. Legitimate git smart-HTTP
//     suffixes (info/refs, git-upload-pack, git-receive-pack, objects/...,
//     HEAD) never contain a second literal ".git", so this does not
//     false-positive on real traffic.
func repoNameFromPath(path string) (repo string, ok bool) {
	rest, ok := strings.CutPrefix(path, gitserver.Prefix+"/")
	if !ok {
		return "", false
	}
	repo, after, found := strings.Cut(rest, ".git")
	if !found || repo == "" {
		return "", false
	}
	if strings.Contains(after, ".git") {
		return "", false
	}
	if !validRepoName.MatchString(repo) {
		return "", false
	}
	return repo, true
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
