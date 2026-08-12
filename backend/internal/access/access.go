// Package access implements the design doc's Faz 3 "proje bazlı
// yetkilendirme": an admin can restrict a specific person to only the
// repositories they've been explicitly granted.
//
// The default is unrestricted. A subject with no entry in the store can see
// every repository — exactly today's behavior, unchanged. That is a
// deliberate choice: unlike internal/deployment's target list (a brand new
// capability that is safely "nothing works" until configured), repository
// visibility is a capability every existing user already has. Defaulting
// restricted-by-default here would lock out the platform's current users
// the moment this package was wired in, before an admin got a chance to
// grant anyone anything back. An admin opts a specific person into a
// restriction; nobody is opted in by this package existing.
package access

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

var ErrInvalidSubject = errors.New("access: subject must not be empty")

// Store persists, per subject, the list of repositories they are
// restricted to. It is safe to call every method on a nil *Store — a nil
// Store behaves as "nobody is restricted," which is what lets every caller
// (repoapi, taskboard, mergerequest, deployment, the RequireRepoAccess
// middleware) treat Access as an always-present, optionally-inert
// collaborator instead of nil-checking it themselves at every call site.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Allowed returns subject's repository allow-list and whether any
// restriction exists at all. restricted=false (regardless of what repos
// contains) means subject can see every repository — the default for
// anyone an admin has never called Set for.
func (s *Store) Allowed(subject string) (repos []string, restricted bool, err error) {
	if s == nil {
		return nil, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, false, err
	}
	list, ok := registry[subject]
	if !ok {
		return nil, false, nil
	}
	return list, true, nil
}

// CanAccess reports whether subject may see repo: true when subject is
// unrestricted, or when repo appears in their allow-list.
func (s *Store) CanAccess(subject, repo string) (bool, error) {
	repos, restricted, err := s.Allowed(subject)
	if err != nil {
		return false, err
	}
	if !restricted {
		return true, nil
	}
	for _, r := range repos {
		if r == repo {
			return true, nil
		}
	}
	return false, nil
}

// Set replaces subject's allow-list with repos, restricting them to
// exactly that set from now on. An empty (non-nil) repos slice restricts
// subject to nothing — deliberately different from never calling Set,
// which leaves them unrestricted (see Allowed).
func (s *Store) Set(subject string, repos []string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	if repos == nil {
		repos = []string{}
	}
	registry[subject] = repos
	return s.save(registry)
}

// Clear removes any restriction on subject, returning them to unrestricted
// (sees every repository).
func (s *Store) Clear(subject string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	delete(registry, subject)
	return s.save(registry)
}

// List returns every subject currently restricted, and their allow-list.
// Subjects with no restriction (the majority, by default) are absent —
// there is nothing meaningful to show an admin about someone who has
// never been restricted.
func (s *Store) List() (map[string][]string, error) {
	if s == nil {
		return map[string][]string{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, err
	}
	return registry, nil
}

func (s *Store) load() (map[string][]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	registry := map[string][]string{}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func (s *Store) save(registry map[string][]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".access-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// RequireRepoAccess returns middleware that blocks a request for a
// repo-scoped route (one with a {repo} path value) unless the
// authenticated caller can see that repository. Must run behind
// auth.RequireAuth. Admins always pass, matching auth.RequireRole's own
// "RoleAdmin always satisfies any check" rule — restricting an admin's own
// visibility would make administering the restriction impossible.
func RequireRepoAccess(store *Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		if user.Role == auth.RoleAdmin {
			next.ServeHTTP(w, r)
			return
		}

		repo := r.PathValue("repo")
		allowed, err := store.CanAccess(user.Subject, repo)
		if err != nil {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			http.Error(w, "403 Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FilterRepos returns the subset of repos that subject may see (or every
// entry, unmodified, when Store is nil or subject is unrestricted) — the
// building block ListAll-style cross-repo endpoints use to keep their
// aggregate view honest instead of leaking items from a restricted repo
// through a route that has no single {repo} for RequireRepoAccess to
// check.
func (s *Store) FilterRepos(subject string, repos []string) ([]string, error) {
	if s == nil {
		return repos, nil
	}
	_, restricted, err := s.Allowed(subject)
	if err != nil {
		return nil, err
	}
	if !restricted {
		return repos, nil
	}

	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		ok, err := s.CanAccess(subject, repo)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, repo)
		}
	}
	return out, nil
}
