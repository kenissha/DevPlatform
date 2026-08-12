// Package users keeps the registry of people who have used the platform.
//
// It is deliberately not an identity store: authentication happens in an
// external system and arrives as a JWT (see internal/auth). This package
// only records who those authenticated people are, so the rest of the
// platform can offer a list of them — an assignee picker, a notification
// recipient — instead of asking everyone to type a username correctly.
//
// Entries are created just-in-time: the first time someone's token is
// accepted, they appear here. That keeps the list honest (it always
// reflects who actually has access) and means nobody has to be added by
// hand before work can be assigned to them. The design doc's Faz 3
// "kişi ekleme" flow, where an admin invites someone up front, layers on
// top of this rather than replacing it.
package users

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrInvalidSubject = errors.New("users: subject must not be empty")

// User is one person known to the platform.
type User struct {
	Subject   string    `json:"subject"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// Store persists the registry as a single JSON file. The whole registry is
// rewritten on each change: it holds one entry per colleague, so the file
// stays small enough that a read-modify-write beats the complexity of
// per-user files or an index.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Upsert records that subject was seen now, creating the entry on first
// sight and refreshing email/role/LastSeen afterwards. Email and role come
// from the token, so a role change in the external identity system is
// picked up on the holder's next request rather than needing a sync.
func (s *Store) Upsert(subject, email, role string) (User, error) {
	if subject == "" {
		return User{}, ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return User{}, err
	}

	now := time.Now().UTC()
	user, exists := registry[subject]
	if !exists {
		user = User{Subject: subject, FirstSeen: now}
	}
	user.Email = email
	user.Role = role
	user.LastSeen = now
	registry[subject] = user

	if err := s.save(registry); err != nil {
		return User{}, err
	}
	return user, nil
}

// Get returns the single user identified by subject, if known. Used to
// resolve a notification recipient (a bare subject) to their email address
// when sending real mail — see internal/notify's EmailLookup.
func (s *Store) Get(subject string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return User{}, false, err
	}
	u, ok := registry[subject]
	return u, ok, nil
}

// List returns every known user, most recently seen first.
func (s *Store) List() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, err
	}

	out := make([]User, 0, len(registry))
	for _, u := range registry {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Subject < out[j].Subject
	})
	return out, nil
}

// load reads the registry. A missing file is an empty registry, not an
// error — nobody has logged in yet.
func (s *Store) load() (map[string]User, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]User{}, nil
		}
		return nil, err
	}

	registry := map[string]User{}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// save writes the registry via a temp file and rename, so an interrupted
// write can't leave a half-written registry behind — unlike the append-only
// audit log, this file is rewritten in place every time.
func (s *Store) save(registry map[string]User) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".users-*.tmp")
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
