package gitserver

import (
	"fmt"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// protectedRefs lists reference names that can never be updated by a
// direct push through this server. Pushing to one of these is expected to
// go through a review/merge flow in a later plan instead.
var protectedRefs = map[plumbing.ReferenceName]bool{
	plumbing.NewBranchReferenceName("main"): true,
}

// protectingLoader wraps a transport.Loader so every storer it returns
// rejects reference updates to protectedRefs.
type protectingLoader struct {
	inner transport.Loader
}

func newProtectingLoader(inner transport.Loader) transport.Loader {
	return &protectingLoader{inner: inner}
}

func (l *protectingLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &protectedStorer{Storer: st}, nil
}

// protectedStorer embeds a real storage.Storer so every method is
// delegated automatically via Go's interface embedding, except the
// reference-write methods, which are overridden to reject protected refs.
type protectedStorer struct {
	storage.Storer
}

func (s *protectedStorer) SetReference(ref *plumbing.Reference) error {
	if protectedRefs[ref.Name()] {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", ref.Name())
	}
	return s.Storer.SetReference(ref)
}

func (s *protectedStorer) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	if protectedRefs[newRef.Name()] {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", newRef.Name())
	}
	return s.Storer.CheckAndSetReference(newRef, old)
}

// RemoveReference also rejects protected refs: receive_pack.go's
// updateReferences calls RemoveReference (not SetReference) for branch
// deletion, so without this override `git push --delete main` would bypass
// the SetReference/CheckAndSetReference guards above.
func (s *protectedStorer) RemoveReference(name plumbing.ReferenceName) error {
	if protectedRefs[name] {
		return fmt.Errorf("gitserver: deleting protected ref %q is not allowed", name)
	}
	return s.Storer.RemoveReference(name)
}
