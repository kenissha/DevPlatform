// Package gitserver exposes DevPlatform's bare git repositories over the
// git smart-HTTP protocol, using go-git's own server implementation
// (github.com/go-git/go-git/v6/backend) rather than hand-rolling the wire
// protocol. See docs/superpowers/specs/2026-08-07-dev-platform-design.md,
// "Git Sunucusu — Teknoloji Kararı", for why.
package gitserver

import (
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

// NewHandler returns an http.Handler serving every bare repository under
// dataDir via the git smart-HTTP protocol. Repository names are resolved
// the same way repostore.Store names them (e.g. "foo" on disk as
// "foo.git"); callers must request "/foo.git/...", not "/foo/...".
//
// The returned handler is wrapped with withReceivePackAuthShim, a
// permanent go-git v6-alpha workaround (see that function's doc comment)
// — it stays regardless of DevPlatform's own auth, since this constructor
// has no guarantee callers wrap it with gitauth.RequireBasicAuth (this
// package's own tests call it directly, unwrapped).
func NewHandler(dataDir string) http.Handler {
	loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
	protected := newProtectingLoader(loader)
	b := backend.New(protected)
	b.Prefix = Prefix
	return withReceivePackAuthShim(b)
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
// This is unrelated to DevPlatform's own auth (internal/gitauth), which
// wraps this handler from the outside in main.go and rejects unauthorized
// requests before they ever reach here — by the time a request reaches
// this shim through that path, it already carries a real, validated
// Authorization header, so the synthetic header below is never applied in
// production. It only fires for callers that invoke NewHandler directly
// without gitauth in front (this package's own integration tests), where
// it supplies a synthetic, unvalidated Authorization header purely so
// go-git's internal sanity check doesn't hang/reject the request; it
// performs no credential validation of its own and grants no capability
// beyond what the wrapping handler already allows. Remove only if a future
// go-git release fixes the underlying 401/WWW-Authenticate gap.
func withReceivePackAuthShim(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Basic YW5vbnltb3VzOg==") // "anonymous:"
		}
		h.ServeHTTP(w, r)
	})
}
