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
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

var (
	ErrInvalidName   = errors.New("repostore: invalid repository name")
	ErrAlreadyExists = errors.New("repostore: repository already exists")
	ErrNotExist      = errors.New("repostore: repository does not exist")
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// reservedNames are Windows-reserved device names. They are not valid file
// or directory names on Windows regardless of extension (e.g. "CON.git" is
// just as reserved as "CON"), so they are rejected up front rather than
// being handed to os.Mkdir where the resulting failure would be an opaque
// OS error instead of ErrInvalidName.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

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
	if reservedNames[strings.ToUpper(name)] {
		return "", ErrInvalidName
	}

	path := filepath.Join(s.rootDir, name+".git")
	if err := os.Mkdir(path, 0o750); err != nil {
		if os.IsExist(err) {
			return "", ErrAlreadyExists
		}
		return "", err
	}

	if _, err := git.PlainInit(path, true, git.WithDefaultBranch(plumbing.NewBranchReferenceName("main"))); err != nil {
		// Best-effort cleanup: remove the directory os.Mkdir just created so
		// the name stays reusable for a future Create call. If cleanup
		// itself fails, there's nothing more productive to do than still
		// return the original PlainInit error to the caller.
		_ = os.RemoveAll(path)
		return "", err
	}

	return path, nil
}

// Open opens an existing repository named name for reading (e.g. computing
// diffs, listing branches). It applies the same name validation as Create,
// so a hostile name can never escape rootDir here either.
func (s *Store) Open(name string) (*git.Repository, error) {
	if !validName.MatchString(name) {
		return nil, ErrInvalidName
	}
	if reservedNames[strings.ToUpper(name)] {
		return nil, ErrInvalidName
	}

	path := filepath.Join(s.rootDir, name+".git")
	repo, err := git.PlainOpen(path)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, ErrNotExist
		}
		return nil, err
	}
	return repo, nil
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
			stripped := e.Name()[:len(e.Name())-len(".git")]
			if stripped == "" {
				// A directory literally named ".git" (not "something.git")
				// would otherwise strip down to an empty string. Create
				// can't produce this (the regex requires at least one
				// character before the suffix), but skip it defensively in
				// case such a directory is ever created some other way.
				continue
			}
			names = append(names, stripped)
		}
	}
	return names, nil
}
