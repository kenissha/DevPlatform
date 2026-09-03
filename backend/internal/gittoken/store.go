// Package gittoken implements per-user credentials for the git
// smart-HTTP endpoints, replacing the single shared
// DEVPLATFORM_GIT_USERNAME/_PASSWORD pair — see
// docs/superpowers/specs/2026-08-17-per-user-git-access-design.md. Each
// person can have any number of active, independently-revocable
// tokens (one per machine/CLI login is the expected pattern — see
// docs/superpowers/specs/2026-09-03-cli-git-login-design.md) — only
// each token's SHA-256 hash is ever persisted, and a raw value is
// returned exactly once, at generation time.
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
	"strings"
	"sync"
	"time"
)

var ErrInvalidSubject = errors.New("gittoken: subject must not be empty")

// tokenBytes is the raw entropy of a generated token before base64
// encoding — 32 bytes (256 bits), the same budget internal/auth's JWT
// secret and internal/secretsvault's key use elsewhere in this codebase.
const tokenBytes = 32

// idBytes is the raw entropy of a token's ID — this only needs to be
// unique per subject, not globally, so it's shorter than tokenBytes.
const idBytes = 8

// Token is one of a subject's active credentials.
type Token struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

// TokenInfo is Token without its Hash — what List returns for the
// panel's "Hesabım" page. The hash never needs to leave this package.
type TokenInfo struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store persists, per subject, the list of their active tokens. Unlike
// internal/access, every caller here already has a concrete Store (see
// cmd/devplatform/main.go) — there is no "optionally inert nil Store"
// case to support.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// now is a seam so tests could freeze CreatedAt if ever needed —
// matches the pattern internal/deploy/versionstore.go already
// established for the same reason.
var now = time.Now

// Generate creates a new random token for subject, labeled label, and
// ADDS it to subject's active tokens — it never invalidates any
// existing token (unlike the single-token model this replaced). This is
// the only moment the raw value exists outside the caller's memory; it
// is never stored and cannot be recovered afterward.
func (s *Store) Generate(subject, label string) (id, rawToken string, err error) {
	if subject == "" {
		return "", "", ErrInvalidSubject
	}

	rawID := make([]byte, idBytes)
	if _, err := rand.Read(rawID); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(rawID)

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	rawToken = base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return "", "", err
	}
	registry[subject] = append(registry[subject], Token{
		ID:        id,
		Hash:      hash(rawToken),
		Label:     label,
		CreatedAt: now().UTC(),
	})
	if err := s.save(registry); err != nil {
		return "", "", err
	}
	return id, rawToken, nil
}

// List returns subject's active tokens, newest first, without their
// hashes.
func (s *Store) List(subject string) ([]TokenInfo, error) {
	if subject == "" {
		return nil, ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, err
	}
	tokens := registry[subject]
	out := make([]TokenInfo, len(tokens))
	for i, t := range tokens {
		// Generate appends, so the stored order is oldest-first — reverse
		// it so List reads newest-first, the order the panel wants.
		out[len(tokens)-1-i] = TokenInfo{ID: t.ID, Label: t.Label, CreatedAt: t.CreatedAt}
	}
	return out, nil
}

// Revoke removes subject's token with the given id, if any. A missing
// id is not an error — idempotent, the same convention
// internal/access.Store.Clear uses.
func (s *Store) Revoke(subject, id string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	tokens := registry[subject]
	out := tokens[:0]
	for _, t := range tokens {
		if t.ID != id {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		delete(registry, subject)
	} else {
		registry[subject] = out
	}
	return s.save(registry)
}

// RevokeAll removes every one of subject's active tokens — "cut off
// this person's git access entirely," what the admin-only
// DELETE /api/git-token/{subject} route performs.
func (s *Store) RevokeAll(subject string) error {
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

// Verify reports whether token is one of subject's active tokens. Load
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

	got := hash(token)
	tokens := registry[subject]
	if len(tokens) == 0 {
		subtle.ConstantTimeCompare([]byte(got), []byte(unknownSubjectHash))
		return false
	}

	matched := false
	for _, t := range tokens {
		// Compare against every stored hash, not just until the first
		// match — stopping early would make timing depend on which
		// token (if any) matched.
		if subtle.ConstantTimeCompare([]byte(got), []byte(t.Hash)) == 1 {
			matched = true
		}
	}
	return matched
}

// unknownSubjectHash is compared against when a subject has zero
// tokens, so a request for a subject that doesn't exist takes the same
// time as one for a subject that exists but sent the wrong token —
// the same discipline internal/gitauth applies elsewhere for this
// package's git Basic-Auth hot path. The value is arbitrary; it can
// never equal a real token's hash (every real hash is the SHA-256 of
// an actual generated token), so this comparison is always expected
// to fail — its only purpose is to cost the same amount of time as a
// real comparison would. Built with strings.Repeat rather than a
// hand-typed literal so its length is exactly right by construction
// (hex.EncodeToString of a sha256.Sum256 is always 64 characters).
var unknownSubjectHash = strings.Repeat("0", 64)

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// legacyMigrationDate is CreatedAt for tokens upgraded from the
// pre-multi-token format (see legacyUpgrade) — the real creation time
// isn't recoverable from that format, so this uses the date that
// format shipped (docs/superpowers/specs/2026-08-17-per-user-git-
// access-design.md) rather than time.Now(), so the value is stable
// across repeated loads instead of drifting to whenever someone
// happens to open the panel.
var legacyMigrationDate = time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

// load reads the registry. A missing file is an empty registry, not an
// error — nobody has generated a token yet. A file written by the
// single-token Store this package replaced (subject -> hash string,
// live in production since 2026-08-17, see git history before
// 2026-09-03) fails to unmarshal into the current shape and is
// transparently upgraded via legacyUpgrade instead — every subject who
// already had a git token before this multi-token plan shipped keeps
// working (both List/Revoke and, critically, git's own Verify auth
// gate) without needing to regenerate.
func (s *Store) load() (map[string][]Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Token{}, nil
		}
		return nil, err
	}
	registry := map[string][]Token{}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		legacy := map[string]string{}
		if legacyErr := json.Unmarshal(data, &legacy); legacyErr != nil {
			return nil, err
		}
		return legacyUpgrade(legacy), nil
	}
	return registry, nil
}

// legacyUpgrade converts a pre-multi-token registry (subject -> single
// hash string) into the current shape. The new ID is derived
// deterministically from the hash (not random) so it stays the same
// across repeated loads without needing to persist the upgrade to disk
// first — List then Revoke against the same ID keeps working even
// before anything writes the file in the new format. The next
// Generate/Revoke/RevokeAll for that subject persists it in the new
// shape via the normal save path, same as any other change.
func legacyUpgrade(legacy map[string]string) map[string][]Token {
	registry := make(map[string][]Token, len(legacy))
	for subject, oldHash := range legacy {
		sum := sha256.Sum256([]byte(oldHash))
		registry[subject] = []Token{{
			ID:        hex.EncodeToString(sum[:idBytes]),
			Hash:      oldHash,
			Label:     "eski anahtar",
			CreatedAt: legacyMigrationDate,
		}}
	}
	return registry
}

// save writes the registry via a temp file and rename, so an interrupted
// write can't leave a half-written registry behind — the same pattern
// internal/access.Store.save uses.
func (s *Store) save(registry map[string][]Token) error {
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
