package deploy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var ErrInvalidRepo = errors.New("deploy: invalid repository name")

// validRepoName mirrors repostore's own validation — this package builds
// filesystem paths from repo names too, and duplicating the check keeps
// this package safe against path traversal on its own, the same
// reasoning taskboard.go and mergerequest.go already documented for
// their own copies of this same regexp.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var validEnvironment = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// VersionStore manages versioned release directories on disk, rooted at
// rootDir, organized as rootDir/<repo>/<environment>/<timestamp>/.
type VersionStore struct {
	rootDir string
}

// NewVersionStore returns a VersionStore rooted at rootDir. rootDir does
// not need to exist yet.
func NewVersionStore(rootDir string) *VersionStore {
	return &VersionStore{rootDir: rootDir}
}

// NewRelease creates and returns the path to a fresh, empty directory for
// a new release of repo in environment. The directory name is a
// nanosecond-precision timestamp, which is both unique enough for this
// package's purposes and sorts correctly as a plain string.
func (s *VersionStore) NewRelease(repo, environment string) (string, error) {
	if !validRepoName.MatchString(repo) {
		return "", ErrInvalidRepo
	}
	if !validEnvironment.MatchString(environment) {
		return "", ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo, environment, releaseName(time.Now()))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

func releaseName(t time.Time) string {
	return t.UTC().Format("20060102T150405.000000000")
}

// List returns the full paths of every release for (repo, environment),
// newest first.
func (s *VersionStore) List(repo, environment string) ([]string, error) {
	if !validRepoName.MatchString(repo) || !validEnvironment.MatchString(environment) {
		return nil, ErrInvalidRepo
	}

	dir := filepath.Join(s.rootDir, repo, environment)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // release names are zero-padded timestamps, so lexical order is chronological order

	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths, nil
}

// Prune deletes every release for (repo, environment) except the keep
// newest ones.
func (s *VersionStore) Prune(repo, environment string, keep int) error {
	releases, err := s.List(repo, environment)
	if err != nil {
		return err
	}
	if len(releases) <= keep {
		return nil
	}
	for _, old := range releases[keep:] {
		if err := os.RemoveAll(old); err != nil {
			return fmt.Errorf("deploy: failed to prune release %q: %w", old, err)
		}
	}
	return nil
}
