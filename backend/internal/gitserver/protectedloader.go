package gitserver

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// protectedRefNames lists reference names (as canonical strings) that can
// never be updated by a direct push through this server. Pushing to one of
// these is expected to go through a review/merge flow in a later plan
// instead.
//
// Compared case-insensitively via isProtectedRef, since go-git's
// filesystem-backed storer resolves refs as filesystem paths and this
// server's target OS (Windows) has a case-insensitive filesystem — an
// exact-case check alone would let "refs/heads/Main" sail past the guard
// while still colliding with and overwriting "refs/heads/main" on disk.
var protectedRefNames = []string{
	plumbing.NewBranchReferenceName("main").String(),
}

// isProtectedRef reports whether name refers to one of protectedRefNames,
// comparing case-insensitively. See protectedRefNames for why.
func isProtectedRef(name plumbing.ReferenceName) bool {
	for _, p := range protectedRefNames {
		if strings.EqualFold(name.String(), p) {
			return true
		}
	}
	return false
}

// protectingLoader wraps a transport.Loader so every storer it returns
// rejects reference updates to protectedRefNames — unless allowProtected
// is true, in which case protection is skipped entirely for every storer
// this loader produces. allowProtected reflects a single request's
// caller (see WithAdmin/IsAdmin): gitserver.NewHandler builds
// a fresh protectingLoader per request, so this is never shared or
// reused across two different callers.
type protectingLoader struct {
	inner          transport.Loader
	allowProtected bool
}

func newProtectingLoader(inner transport.Loader, allowProtected bool) transport.Loader {
	return &protectingLoader{inner: inner, allowProtected: allowProtected}
}

func (l *protectingLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	if l.allowProtected {
		return st, nil
	}
	return &protectedStorer{Storer: st}, nil
}

// protectedStorer embeds a real storage.Storer so every method is
// delegated automatically via Go's interface embedding, except the
// reference-write methods, which are overridden to reject protected refs.
// protectingLoader only ever constructs one of these when allowProtected
// is false, so protectedStorer itself doesn't need its own bypass flag —
// its mere existence means "this request's caller may not touch
// protected refs."
type protectedStorer struct {
	storage.Storer
}

func (s *protectedStorer) SetReference(ref *plumbing.Reference) error {
	if isProtectedRef(ref.Name()) {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", ref.Name())
	}
	return s.Storer.SetReference(ref)
}

// CheckAndSetReference is defensive/future-proofing: go-git v6-alpha.5's
// receive_pack.go updateReferences only ever calls SetReference (for
// create/update) and RemoveReference (for delete) on the live push path,
// never this method, so it is not exercised by any test today. It's kept
// in sync with the other two overrides via isProtectedRef so it can't
// silently drift out of sync with the case-insensitivity fix if a future
// go-git version starts using it.
func (s *protectedStorer) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	if isProtectedRef(newRef.Name()) {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", newRef.Name())
	}
	return s.Storer.CheckAndSetReference(newRef, old)
}

// RemoveReference also rejects protected refs: receive_pack.go's
// updateReferences calls RemoveReference (not SetReference) for branch
// deletion, so without this override `git push --delete main` would bypass
// the SetReference/CheckAndSetReference guards above.
func (s *protectedStorer) RemoveReference(name plumbing.ReferenceName) error {
	if isProtectedRef(name) {
		return fmt.Errorf("gitserver: deleting protected ref %q is not allowed", name)
	}
	return s.Storer.RemoveReference(name)
}
