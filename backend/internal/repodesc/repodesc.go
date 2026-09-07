// Package repodesc stores a one-line description per repository — the
// "what is this project" text a git repository has nowhere to keep. Git
// itself has no such field (GitHub's repo description lives in GitHub's
// database, not in the repo), so a repository that isn't obvious from its
// name is unreadable to anyone who didn't set it up.
//
// Like internal/displaynames and unlike internal/access, this is not a
// security boundary: a description changes what text is shown, never what
// anyone can do or see. A nil Store or an unset repo simply means "no
// description," exactly like an empty store would.
package repodesc

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kenissha/DevPlatform/backend/internal/atomicfile"
)

var (
	ErrInvalidRepo   = errors.New("repodesc: repo must not be empty")
	ErrTooLong       = errors.New("repodesc: description is too long")
	ErrNotConfigured = errors.New("repodesc: no store configured")
)

// MaxLength caps a description. This is a subtitle, not a README — the
// cards and page headers that render it give it one line, and text past
// that point is truncated on screen anyway. Rejecting it at the door
// beats storing something no reader will ever see in full.
const MaxLength = 200

// Store persists repo→description in a single JSON file, read fresh from
// disk on every call — same shape and atomic-write discipline as
// internal/displaynames.Store.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Get returns repo's description, or "" if it has none. Safe to call on a
// nil Store.
func (s *Store) Get(repo string) string {
	if s == nil || repo == "" {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return ""
	}
	return registry[repo]
}

// Set records repo's description, replacing any previous one. An empty
// description clears the entry rather than storing a blank string, so
// "cleared" and "never set" stay the same state on disk.
//
// Safe to call on a nil Store, which reports ErrNotConfigured rather than
// panicking — the read paths treat nil as "no descriptions", and a write
// path that crashed the process on the same value would be a trap.
func (s *Store) Set(repo, description string) error {
	if s == nil {
		return ErrNotConfigured
	}
	if repo == "" {
		return ErrInvalidRepo
	}
	description = strings.TrimSpace(description)
	if len([]rune(description)) > MaxLength {
		return ErrTooLong
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	if description == "" {
		delete(registry, repo)
	} else {
		registry[repo] = description
	}
	return s.save(registry)
}

// List returns every repo with a description. Safe to call on a nil Store
// — returns an empty map, which callers merge into a repo list as "no
// descriptions configured".
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

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".repodesc-*.tmp")
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
	return atomicfile.Rename(tmpName, s.path)
}
