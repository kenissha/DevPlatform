// Package gittoken implements per-user credentials for the git
// smart-HTTP endpoints, replacing the single shared
// DEVPLATFORM_GIT_USERNAME/_PASSWORD pair — see
// docs/superpowers/specs/2026-08-17-per-user-git-access-design.md. Each
// person gets at most one active, high-entropy token; only its SHA-256
// hash is ever persisted, and the raw value is returned exactly once, at
// generation time.
package gittoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrInvalidSubject = errors.New("gittoken: subject must not be empty")

// tokenBytes is the raw entropy of a generated token before base64
// encoding — 32 bytes (256 bits), the same budget internal/auth's JWT
// secret and internal/secretsvault's key use elsewhere in this codebase.
const tokenBytes = 32

// unknownSubjectHash is compared against when subject has no stored
// hash, so Verify always runs ConstantTimeCompare against a same-length
// buffer regardless of whether subject exists — an unknown-subject
// rejection and a wrong-token rejection take the same amount of time,
// the same discipline internal/gitauth (which this package replaces)
// already applied to username/password comparison.
var unknownSubjectHash = hex.EncodeToString(make([]byte, sha256.Size))

// Store persists, per subject, the SHA-256 hash of their single active
// git token. Unlike internal/access, every caller here already has a
// concrete Store (see cmd/devplatform/main.go) — there is no
// "optionally inert nil Store" case to support.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Generate creates a new random token for subject, persists its hash
// (overwriting any previous token — a subject has at most one active
// token, the same "generating a new one invalidates the old" model as a
// password reset), and returns the raw token. This is the only moment
// the raw value exists outside the caller's memory; it is never stored
// and cannot be recovered afterward.
func (s *Store) Generate(subject string) (string, error) {
	if subject == "" {
		return "", ErrInvalidSubject
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return "", err
	}
	registry[subject] = hash(token)
	if err := s.save(registry); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke removes subject's stored token hash, if any. A subject with no
// stored token is not an error — revoking is idempotent, the same
// "removing something already absent succeeds" convention
// internal/access.Store.Clear uses.
func (s *Store) Revoke(subject string) error {
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

// Verify reports whether token is subject's current active token. Load
// errors are treated as "not verified" rather than surfaced — this runs
// on the hot path of every git request as an HTTP Basic Auth gate, where
// the only two outcomes that matter are "allowed" or "401"; a corrupt
// store file must fail closed, not 500.
func (s *Store) Verify(subject, token string) bool {
	s.mu.Lock()
	registry, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return false
	}

	want, ok := registry[subject]
	if !ok {
		want = unknownSubjectHash
	}
	got := hash(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// load reads the registry. A missing file is an empty registry, not an
// error — nobody has generated a token yet.
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

// save writes the registry via a temp file and rename, so an interrupted
// write can't leave a half-written registry behind — the same pattern
// internal/access.Store.save and internal/users.Store.save use.
func (s *Store) save(registry map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".git-tokens-*.tmp")
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
