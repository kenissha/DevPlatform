// Package displaynames lets an admin give a person a friendlier
// subject→"Ad Soyad" display name than whatever their SSO's JWT carries.
// The external identity system's JWT (see backend/internal/auth) only
// carries subject and email — no name claim — so this is the one place a
// human-readable name can be attached to a subject at all. Unlike
// internal/access, this is not a security boundary: an override here only
// changes what text is shown, never what a person can do or see, so there
// is no nil-means-locked-down posture to preserve — a nil Store or an
// unset subject simply means "show the fallback," exactly like an empty
// store would.
package displaynames

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrInvalidSubject = errors.New("displaynames: subject must not be empty")

// Store persists, per subject, an optional display-name override in a
// single JSON file — same shape and atomic-write discipline as
// internal/access.Store, read fresh from disk on every call.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Get returns subject's display-name override, or fallback (typically
// their email) if none is set. Safe to call on a nil Store — it behaves
// as "no overrides configured," always returning fallback.
func (s *Store) Get(subject, fallback string) string {
	if s == nil || subject == "" {
		return fallback
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return fallback
	}
	name, ok := registry[subject]
	if !ok || name == "" {
		return fallback
	}
	return name
}

// Set records subject's display-name override, replacing any previous one.
func (s *Store) Set(subject, name string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	registry[subject] = name
	return s.save(registry)
}

// Clear removes subject's override, if any. A subject with no override is
// not an error — matches internal/access.Store.Clear's idempotent-remove
// behavior.
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

// List returns every subject with an override, for the admin panel's
// management table. Safe to call on a nil Store — returns an empty map.
func (s *Store) List() (map[string]string, error) {
	if s == nil {
		return map[string]string{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	registry := map[string]string{}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func (s *Store) save(registry map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".displaynames-*.tmp")
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
