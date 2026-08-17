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

// allowedSuffixes enumerates, as anchored regexps, every request-path
// SHAPE go-git's smart-HTTP backend actually recognizes (verified against
// the pinned github.com/go-git/go-git/v6@v6.0.0-alpha.5 module's
// backend/http.go httpServices table — see repoNameFromPath's doc comment
// for details). This covers only the path-pattern half of that table, not
// the (pattern, method) pairs it's actually keyed on — e.g. "git-upload-pack"
// and "git-receive-pack" are POST-only there, GET is rejected downstream
// with 405 — because method mismatches don't grant cross-repo access and
// are irrelevant to the authorization-bypass class this allow-list exists
// to close; go-git's own ServeHTTP still enforces the method for each
// route after this middleware passes the request through. A remainder
// that matches none of these path shapes is rejected outright. This is an
// allow-list of the finite, fully-specified set of legitimate shapes —
// not a denylist of known-bad ones — so it does not depend on correctly
// anticipating every OS/filesystem/encoding quirk a downstream parser
// might apply (an earlier version here rejected paths that "looked"
// unclean via Go's slash-only path.Clean, and was proven bypassable by a
// backslash-based traversal component that path.Clean never recognizes
// as a separator but Windows/go-billy does). Every pattern below is
// either a fixed literal or a narrow [0-9a-f]{n,m} hex charset — neither
// can ever spell a path separator or ".." under any interpretation, so a
// match here is safe regardless of how any downstream code parses the
// string.
var allowedSuffixes = []*regexp.Regexp{
	regexp.MustCompile(`^HEAD$`),
	regexp.MustCompile(`^info/refs$`),
	regexp.MustCompile(`^objects/info/alternates$`),
	regexp.MustCompile(`^objects/info/http-alternates$`),
	regexp.MustCompile(`^objects/info/packs$`),
	regexp.MustCompile(`^objects/[0-9a-f]{2}/[0-9a-f]{38,62}$`),
	regexp.MustCompile(`^objects/pack/pack-[0-9a-f]{40,64}\.pack$`),
	regexp.MustCompile(`^objects/pack/pack-[0-9a-f]{40,64}\.idx$`),
	regexp.MustCompile(`^git-upload-pack$`),
	regexp.MustCompile(`^git-receive-pack$`),
	regexp.MustCompile(`^git-upload-archive$`),
}

// isKnownSuffix reports whether remainder exactly matches one of
// allowedSuffixes, or is empty (a bare "/git/<name>.git" request with
// nothing after it — no attacker-controlled content possible here at
// all; go-git's own route table has nothing that matches an empty
// remainder either, so this is never forwarded anywhere meaningful, it
// simply isn't a vector).
func isKnownSuffix(remainder string) bool {
	if remainder == "" {
		return true
	}
	for _, re := range allowedSuffixes {
		if re.MatchString(remainder) {
			return true
		}
	}
	return false
}

// repoNameFromPath extracts "foo" from a git smart-HTTP request path like
// "/git/foo.git/info/refs" (gitserver.Prefix + "/" + name + ".git" +
// anything). It parses by path segment, not by searching for ".git"
// anywhere in the string: only the FIRST segment is ever treated as the
// repo name, it must exactly equal "<name>.git" with name matching
// repostore's own naming rule, and everything after it must exactly
// match one of allowedSuffixes.
//
// Two earlier approaches in this same spot were both denylists — "reject
// paths that look bad" — and both were proven bypassable, because a
// denylist requires correctly anticipating every shape a downstream
// parser might treat specially, which is an open-ended list:
//  1. Rejecting a second literal ".git" in the remainder missed that
//     gitserver.NewHandler's loader runs strict=false and auto-appends
//     ".git" during resolution, so a target repo need not appear with a
//     ".git" suffix in the request path at all
//     ("/git/allowed.git/../secret/info/refs" resolves to the on-disk
//     "secret.git" even though "secret" never appears with ".git").
//  2. Rejecting a remainder that isn't already path.Clean missed that
//     path.Clean is deliberately slash-only (matching net/url's
//     decoding, which never introduces percent-encoding asymmetry), but
//     go-billy's Windows filesystem layer treats backslash as a
//     separator too — so "..\\secret.git" was never recognized as a
//     traversal by path.Clean, yet still resolved to the sibling repo on
//     disk.
//
// This version instead uses an allow-list (isKnownSuffix): the remainder
// must exactly match one of a small, fixed, anchored set of patterns
// representing every request shape go-git's smart-HTTP backend actually
// recognizes (backend/http.go's httpServices table). Every entry is
// either a fixed literal or a narrow hex character class, neither of
// which can ever spell a "..", "/", "\", or any other separator under
// any encoding or OS interpretation — so there's no partial-match slack
// left for a parser to disagree about, because "is this safe" is never
// delegated to any downstream interpretation; it's exact recognition
// against a finite, fully-specified language instead. Request
// reconstruction (rebuilding a clean request instead of forwarding the
// original path) is therefore not needed either: the validated string is
// already provably safe under any downstream reinterpretation, since an
// exhaustive anchored match leaves nothing to reinterpret.
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
	if !isKnownSuffix(remainder) {
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
