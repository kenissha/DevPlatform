// Package repostore manages bare git repositories on disk. Every
// repository name is validated against a strict allow-list before it is
// ever used to build a filesystem path, so a hostile name (e.g. containing
// ".." or "/") can never escape the configured root directory.
package repostore

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v5"
)

var (
	ErrInvalidName   = errors.New("repostore: invalid repository name")
	ErrAlreadyExists = errors.New("repostore: repository already exists")
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Store manages bare git repositories rooted at a single directory on disk.
type Store struct {
	rootDir string
}

// New returns a Store rooted at rootDir. rootDir does not need to exist yet.
func New(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// Create initializes a new bare git repository named name and returns its
// path on disk. name must match ^[a-zA-Z0-9_-]+$ (letters, digits, dash,
// underscore) — anything else is rejected before it reaches the filesystem.
func (s *Store) Create(name string) (string, error) {
	if !validName.MatchString(name) {
		return "", ErrInvalidName
	}

	path := filepath.Join(s.rootDir, name+".git")
	if _, err := os.Stat(path); err == nil {
		return "", ErrAlreadyExists
	}

	if _, err := git.PlainInit(path, true); err != nil {
		return "", err
	}

	return path, nil
}

// List returns the names of all repositories currently in the store.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	names := []string{}
	for _, e := range entries {
		if e.IsDir() && filepath.Ext(e.Name()) == ".git" {
			names = append(names, e.Name()[:len(e.Name())-len(".git")])
		}
	}
	return names, nil
}
