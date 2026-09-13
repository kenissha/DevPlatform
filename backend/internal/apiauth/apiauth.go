// Package apiauth lets a route accept either of the two credentials a
// person already has: the panel's SSO-issued JWT, or the git token
// devplatform-login put on their machine.
//
// It exists so a program running on somebody's computer — the MCP server
// that puts the task board in front of Claude (see cmd/devplatform-mcp) —
// can call the task API as them without inventing a third credential.
//
// Why reuse the git token rather than mint a new "API key":
//
//   - It already exists and is already managed. devplatform-login mints
//     it, DPAPI protects it, the panel lists and revokes it. A second
//     token would need all of that again, plus somewhere to store it,
//     plus a person to remember it exists.
//   - It is not a weaker credential than the alternative. A git token
//     already grants push access to every repository its owner can see —
//     which is to say, the ability to change code that gets deployed.
//     Reading and writing tasks is strictly less than that, so widening
//     it here does not widen what a leaked one can do in any way that
//     matters.
//   - It is not the person's AD password. The password is never written
//     to disk (see cmd/devplatform-login/login.go); the token is random
//     bytes, and the server keeps only their SHA-256. A stolen token is
//     revocable from the panel without anybody changing a password.
//
// The two credentials arrive under different HTTP auth schemes — Bearer
// for the JWT, Basic for the token — so nothing is ever guessed at. Basic
// is what git already sends and what devplatform-login already prints, so
// a caller holding the cached credential has nothing to convert.
package apiauth

import (
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// TokenVerifier is gittoken.Store's Verify, narrowed to what this package
// uses. An interface rather than the concrete store so apiauth's tests do
// not need a token registry on disk, and so the dependency runs one way:
// apiauth knows it needs something that can check a token, not what a git
// token is.
type TokenVerifier interface {
	Verify(subject, token string) bool
}

// PeopleStore is users.Store's Get. Needed because a git token carries no
// role — the JWT has one in its claims, the token is just a secret — so
// the role has to be looked up.
type PeopleStore interface {
	Get(subject string) (users.User, bool, error)
}

// Require wraps next so it accepts either credential, attaching the same
// auth.User to the request context either way. Everything downstream —
// access.RequireRepoAccess, auth.RequireRole, every handler that calls
// auth.UserFromContext — works unchanged and cannot tell which credential
// was used. That is the point: this is an authentication change, not an
// authorization one.
//
// tokens or people being nil disables the token path entirely and leaves
// the route JWT-only, which is what a deployment that has not wired the
// git token store should get.
func Require(secret []byte, tokens TokenVerifier, people PeopleStore, next http.Handler) http.Handler {
	jwtOnly := auth.RequireAuth(secret, next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Basic is checked first and only Basic reaches the token path:
		// the schemes are disjoint, so a malformed JWT never falls
		// through to a token lookup and a wrong token never gets a
		// second chance as a JWT. An attacker cannot use a failure in
		// one scheme to probe the other.
		subject, token, isBasic := r.BasicAuth()
		if !isBasic {
			jwtOnly.ServeHTTP(w, r)
			return
		}

		if tokens == nil || people == nil || !tokens.Verify(subject, token) {
			unauthorized(w)
			return
		}

		u := auth.User{Subject: subject, Role: auth.RoleDeveloper}
		if people != nil {
			record, found, err := people.Get(subject)
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			}
			if found {
				u.Email = record.Email
				// Anything that is not literally the admin role is a
				// developer. A registry value nobody recognises must not
				// become a privilege — an unknown string is the one case
				// where guessing upward is unrecoverable.
				if record.Role == string(auth.RoleAdmin) {
					u.Role = auth.RoleAdmin
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

// unauthorized answers a failed token the way git's own endpoints do,
// including the challenge header — a credential helper that gets a bare
// 401 without one does not know it should offer credentials.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform"`)
	http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
}
