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

// now is a seam for tests to freeze the clock so a genuine timestamp
// collision in NewRelease can be forced deterministically instead of
// relying on real clock timing. Production code always uses the real
// time.Now.
var now = time.Now

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

	parent := filepath.Join(s.rootDir, repo, environment)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", err
	}

	// Retry on the rare chance two releases would otherwise land on the
	// same formatted timestamp (coarse wall-clock resolution, load, a VM
	// clock, etc.). os.Mkdir — not os.MkdirAll — makes the check-then-
	// create atomic and surfaces a collision as os.IsExist instead of
	// silently reusing (and corrupting) the existing release directory.
	// Mirrors taskboard.go's Create: a handful of attempts, retried on
	// os.IsExist, with a suffix added after the first attempt so a retry
	// is guaranteed to target a different name.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		name := releaseName(now())
		if attempt > 0 {
			name = fmt.Sprintf("%s-%d", name, attempt)
		}
		dir := filepath.Join(parent, name)
		err := os.Mkdir(dir, 0o750)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
		lastErr = err
	}
	return "", fmt.Errorf("deploy: failed to allocate a unique release directory after 5 attempts: %w", lastErr)
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
