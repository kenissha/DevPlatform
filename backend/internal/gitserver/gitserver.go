// Package gitserver exposes DevPlatform's bare git repositories over the
// git smart-HTTP protocol, using go-git's own server implementation
// (github.com/go-git/go-git/v6/backend) rather than hand-rolling the wire
// protocol. See docs/superpowers/specs/2026-08-07-dev-platform-design.md,
// "Git Sunucusu — Teknoloji Kararı", for why.
package gitserver

import (
	"context"
	"net/http"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// Prefix is the URL path prefix under which the git smart-HTTP endpoints
// are served. A request for repo "foo" is reached at Prefix+"/foo.git/...".
// This must stay in sync with the "/git/" mux pattern in
// internal/server.NewRouter — see that function's comment.
const Prefix = "/git"

type contextKey int

const adminContextKey contextKey = iota

// WithAdmin returns a copy of ctx recording whether the caller has Admin
// privileges. gittoken.RequireTokenAndAccess calls this (it already
// looks up the caller's role to decide the repo-access check) before
// invoking this package's handler; NewHandler reads it back per request
// to decide whether protected refs (e.g. main) can be written directly —
// an Admin can push straight to main (the review flow becomes optional
// for them, since they already are the review step), a non-Admin still
// cannot, matching internal/mergerequest's "İnceleme İsteği" flow.
func WithAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, adminContextKey, admin)
}

// IsAdmin reports whether ctx carries WithAdmin(true). A context that
// never went through WithAdmin (e.g. this package's own tests, which
// call NewHandler directly without gittoken in front) reports false —
// the safe, protected-by-default outcome. Exported as the read half of
// the WithAdmin/IsAdmin pair (mirroring internal/auth's
// RequireAuth/UserFromContext) so other packages — e.g. gittoken's own
// tests, verifying RequireTokenAndAccess actually sets this — can
// observe it without gitserver needing test-only exports.
func IsAdmin(ctx context.Context) bool {
	admin, _ := ctx.Value(adminContextKey).(bool)
	return admin
}

// NewHandler returns an http.Handler serving every bare repository under
// dataDir via the git smart-HTTP protocol. Repository names are resolved
// the same way repostore.Store names them (e.g. "foo" on disk as
// "foo.git"); callers must request "/foo.git/...", not "/foo/...".
//
// The loader/backend chain is built fresh for every request (rather than
// once at startup) so protection can depend on that request's own caller
// (IsAdmin) — none of transport.NewFilesystemLoader,
// newProtectingLoader, newScanningLoader, or backend.New do any I/O of
// their own at construction time, so this costs no more per request than
// the git operation itself already does.
//
// The returned handler is wrapped with withReceivePackAuthShim, a
// permanent go-git v6-alpha workaround (see that function's doc comment)
// — it stays regardless of DevPlatform's own auth, since this constructor
// has no guarantee callers wrap it with gittoken.RequireTokenAndAccess
// (this package's own tests call it directly, unwrapped).
func NewHandler(dataDir string) http.Handler {
	return withReceivePackAuthShim(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin := IsAdmin(r.Context())
		loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
		protected := newProtectingLoader(loader, admin)
		scanned := newScanningLoader(protected)
		b := backend.New(scanned)
		b.Prefix = Prefix
		b.ServeHTTP(w, r)
	}))
}

// withReceivePackAuthShim works around a gap in go-git v6-alpha.5's
// backend.Backend.ServeHTTP: it unconditionally returns 401 for any
// git-receive-pack (push) request that lacks an Authorization header, but
// never sends a WWW-Authenticate challenge back — so no real git client
// ever retries with credentials (confirmed with GIT_CURL_VERBOSE=1: git
// treats the header-less 401 as terminal, even with credentials embedded
// in the remote URL, and never attempts to send Authorization at all).
// Backend has no exported field or hook to disable or satisfy this check
// (see its Loader/ErrorLog/Prefix field list) — it's simply not
// configurable in this alpha release.
//
// This is unrelated to DevPlatform's own auth (internal/gittoken), which
// wraps this handler from the outside in main.go and rejects unauthorized
// requests before they ever reach here — by the time a request reaches
// this shim through that path, it already carries a real, validated
// Authorization header, so the synthetic header below is never applied in
// production. It only fires for callers that invoke NewHandler directly
// without gittoken in front (this package's own integration tests), where
// it supplies a synthetic, unvalidated Authorization header purely so
// go-git's internal sanity check doesn't hang/reject the request; it
// performs no credential validation of its own and grants no capability
// beyond what the wrapping handler already allows. WARNING: this means an
// unwrapped NewHandler() (i.e. not behind gittoken.RequireTokenAndAccess
// or equivalent) allows fully anonymous push — the shim removes go-git's own
// incidental header-less-401 barrier, so any future caller that mounts
// NewHandler's output directly (e.g. a second, intentionally read-only
// route reusing this handler) must not skip real authentication. Remove
// only if a future go-git release fixes the underlying 401/WWW-Authenticate
// gap.
func withReceivePackAuthShim(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Basic YW5vbnltb3VzOg==") // "anonymous:"
		}
		h.ServeHTTP(w, r)
	})
}
