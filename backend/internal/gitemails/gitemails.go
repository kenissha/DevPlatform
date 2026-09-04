// Package gitemails records the extra git author addresses a person
// commits under.
//
// A git commit carries no account, only whatever `git config user.email`
// was set to on the machine that made it — and that is very often not
// the address the platform knows someone by (theirs arrives from the
// SSO JWT, see internal/auth). Without a way to say "these addresses
// are also me", the contribution graph in the panel silently shows
// nothing for anyone whose git config differs, which is the common case
// rather than the exception. This is the same problem GitHub solves by
// letting an account list several verified emails.
//
// Not a security boundary: an entry here only widens which commits a
// person sees counted as their own in their own graph. It grants no
// access and reveals nothing new — who commits, and when, is already
// visible to any authenticated caller through the contributors and
// audit views. Addresses are therefore taken at their word rather than
// being verified by a round-trip email, which a 2-person internal team
// behind SSO does not need.
package gitemails

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var (
	ErrInvalidSubject = errors.New("gitemails: subject must not be empty")
	ErrInvalidEmail   = errors.New("gitemails: not a valid email address")
)

// Store persists, per subject, the list of extra git author addresses
// they claim — one JSON file, read fresh on every call, same shape and
// atomic-write discipline as internal/displaynames.Store.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// normalise lowercases and trims an address. Every address is stored
// this way so that comparing them later (and de-duplicating them here)
// is a plain string match — see gitstats.ActivityByAuthors, which
// compares against exactly these values.
func normalise(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// valid is a deliberately loose check: exactly one "@", something on
// each side of it, and no spaces. It exists to keep obvious junk out of
// the list, not to decide what a deliverable address is — nothing is
// ever sent to these, they are only ever compared against what git
// stamped into a commit.
func valid(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	local, domain, found := strings.Cut(email, "@")
	return found && local != "" && domain != "" && !strings.Contains(domain, "@")
}

// List returns subject's registered addresses, already normalised.
// Safe to call on a nil Store — it behaves as an empty registry.
func (s *Store) List(subject string) ([]string, error) {
	if s == nil {
		return []string{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, err
	}
	out := slices.Clone(registry[subject])
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// Add registers email for subject. Adding an address that is already
// listed succeeds without duplicating it.
func (s *Store) Add(subject, email string) error {
	if subject == "" {
		return ErrInvalidSubject
	}
	addr := normalise(email)
	if !valid(addr) {
		return ErrInvalidEmail
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	if slices.Contains(registry[subject], addr) {
		return nil
	}
	registry[subject] = append(registry[subject], addr)
	return s.save(registry)
}

// Remove drops email from subject's list. An address (or subject) that
// isn't there is not an error — the caller's intent is already met.
func (s *Store) Remove(subject, email string) error {
	if subject == "" {
		return ErrInvalidSubject
	}
	addr := normalise(email)

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(registry[subject]), func(e string) bool { return e == addr })
	if len(kept) == len(registry[subject]) {
		return nil
	}
	if len(kept) == 0 {
		delete(registry, subject)
	} else {
		registry[subject] = kept
	}
	return s.save(registry)
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

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".gitemails-*.tmp")
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
