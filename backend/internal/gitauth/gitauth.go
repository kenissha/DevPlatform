// Package gitauth provides a minimal HTTP Basic Auth gate for the git
// smart-HTTP endpoints. This is a deliberate placeholder: a future plan
// replaces the credential check with real Active Directory authentication
// without changing this package's call site in main.go.
package gitauth

import (
	"crypto/subtle"
	"net/http"
)

// RequireBasicAuth wraps next with an HTTP Basic Auth check against a
// single configured username/password. Requests without valid credentials
// receive 401 with a WWW-Authenticate challenge, matching what a `git`
// client expects in order to prompt for credentials.
func RequireBasicAuth(username, password string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !constantTimeEqual(user, username) || !constantTimeEqual(pass, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform Git"`)
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
