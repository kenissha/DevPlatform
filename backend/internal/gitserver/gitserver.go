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
// temporary go-git v6-alpha auth-header workaround (see that function's
// doc comment) — revisit it when real authentication is added.
func NewHandler(dataDir string) http.Handler {
	loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
	b := backend.New(loader)
	b.Prefix = Prefix
	return withReceivePackAuthShim(b)
}

// TODO(task-3): remove this shim once real authentication is wired in.
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
// DevPlatform has no authentication yet — real auth is a later task (see
// docs/superpowers/sdd plan for this feature). Until then every push
// should be allowed, which is what this handler is supposed to do without
// this shim. This shim supplies a synthetic, unvalidated Authorization
// header so go-git's internal sanity check doesn't block that intended
// behavior; it performs no credential validation and grants no elevated
// capability beyond what the handler already grants everyone. Remove this
// once real authentication replaces it.
func withReceivePackAuthShim(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Basic YW5vbnltb3VzOg==") // "anonymous:"
		}
		h.ServeHTTP(w, r)
	})
}
