package secretsvault

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrInvalidRepo = errors.New("secretsvault: invalid repository or environment name")
	ErrNotFound    = errors.New("secretsvault: not found")
)

// validName mirrors the same allow-list pattern used throughout this
// codebase (repostore, taskboard, internal/deploy's VersionStore) for any
// identifier that becomes part of a filesystem path.
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Store persists encrypted secrets on disk, one file per (repo,
// environment) pair, organized the same way internal/deploy's
// VersionStore organizes release directories.
type Store struct {
	rootDir string
	key     []byte
}

// NewStore returns a Store rooted at rootDir, encrypting with key.
// rootDir does not need to exist yet.
func NewStore(rootDir string, key []byte) *Store {
	return &Store{rootDir: rootDir, key: key}
}

// Put encrypts plaintext and writes it to disk for (repo, environment),
// overwriting any existing file for that pair.
func (s *Store) Put(repo, environment string, plaintext []byte) error {
	path, err := s.path(repo, environment)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	encrypted, err := Encrypt(plaintext, s.key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o640)
}

// Get reads and decrypts the secrets file for (repo, environment).
// Returns ErrNotFound if nothing has been Put for that pair yet.
func (s *Store) Get(repo, environment string) ([]byte, error) {
	path, err := s.path(repo, environment)
	if err != nil {
		return nil, err
	}
	encrypted, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return Decrypt(encrypted, s.key)
}

func (s *Store) path(repo, environment string) (string, error) {
	if !validName.MatchString(repo) || !validName.MatchString(environment) {
		return "", ErrInvalidRepo
	}
	return filepath.Join(s.rootDir, repo, environment, "appsettings.json.enc"), nil
}
