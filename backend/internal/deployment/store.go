// Package deployment implements the design doc's "onay sonrası otomatik"
// deploy flow: a Developer opens a deploy request naming a repo, an
// environment, and the branch to release; an Admin reviews and approves
// it; approval runs internal/deploy's already-proven Pipeline
// (checkout → build → version → IIS swap, with secrets injected from
// internal/secretsvault) and records the outcome. It is the same
// review-then-act shape internal/mergerequest already established for
// code review, applied to releases instead of merges.
package deployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

var ErrNoTarget = errors.New("deployment: no deploy target configured for this repo and environment")

// Target is one (repo, environment) pair this platform is allowed to
// deploy, and exactly how: which build recipe, which IIS site, and which
// relative path inside the release a decrypted secrets file should land
// at (empty means no secrets are injected — not every environment needs
// one, e.g. a test environment with no real credentials).
//
// SiteName is the field the security boundary actually runs through:
// TargetStore.Set only accepts a SiteName present in the caller-supplied
// allowedSites set — see docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
// "Güvenlik" section for why that set is loaded from a separate,
// ops-only file (DEVPLATFORM_ALLOWED_SITES_FILE) that no API here can
// ever write to, even though the rest of a Target is fully panel-managed.
type Target struct {
	Repo          string        `json:"repo"`
	Environment   string        `json:"environment"`
	Recipe        deploy.Recipe `json:"recipe"`
	SiteName      string        `json:"siteName"`
	SecretsTarget string        `json:"secretsTarget,omitempty"`
	KeepVersions  int           `json:"keepVersions"`
}

// validRepoName is not redeclared here — it already exists as a
// package-level var in deployment.go (`var validRepoName =
// regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)`, used by the deploy-request
// side of this package), and Go's flat package namespace means a second
// `var validRepoName` in this file would be a compile error
// ("validRepoName redeclared"). validateTarget below calls that
// existing var directly.
//
// validEnvironmentName mirrors validRepoName: an environment name ends up
// in release directory paths (see deploy.VersionStore) just like a repo
// name does, so it gets the same allow-list rather than a looser one.
var validEnvironmentName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateTarget rejects a Target that would be unsafe or ambiguous to
// deploy. It is the single-entry counterpart to what used to be a
// whole-file bulk validator (ValidateTargets, now removed): TargetStore.Set
// calls it on every write instead of once at file-load time, and there
// is no duplicate-entry check anymore because (Repo, Environment) is the
// map-like key Set replaces by, not something that can silently
// duplicate. allowedSites must contain SiteName — the load-bearing check
// that keeps this API from ever letting a caller point a deploy target
// at an IIS site an operator hasn't already approved by hand.
func validateTarget(t Target, allowedSites map[string]bool) error {
	switch {
	case !validRepoName.MatchString(t.Repo):
		return fmt.Errorf("deployment: invalid repo %q", t.Repo)
	case !validEnvironmentName.MatchString(t.Environment):
		return fmt.Errorf("deployment: invalid environment %q", t.Environment)
	case t.Recipe != deploy.RecipeDotnet && t.Recipe != deploy.RecipeNpm:
		return fmt.Errorf("deployment: unknown recipe %q", t.Recipe)
	case strings.TrimSpace(t.SiteName) == "":
		return fmt.Errorf("deployment: siteName is required")
	case !allowedSites[t.SiteName]:
		return fmt.Errorf("deployment: %q is not an approved IIS site — ask an operator to add it to the allowed-sites file", t.SiteName)
	case t.SecretsTarget != "" && !filepath.IsLocal(t.SecretsTarget):
		return fmt.Errorf("deployment: secretsTarget %q must be a relative path inside the release", t.SecretsTarget)
	}
	return nil
}

// TargetStore persists deploy targets as a JSON array of Target under a single
// file — the same shape the old ops-edited DEVPLATFORM_DEPLOY_TARGETS_FILE
// used, now read fresh on every call (like internal/access.Store) and
// writable through Set/Delete instead of loaded once at process startup.
type TargetStore struct {
	path string
	mu   sync.Mutex
}

// NewTargetStore returns a TargetStore backed by the file at path. The file does not
// need to exist yet — a missing file behaves as zero configured targets.
func NewTargetStore(path string) *TargetStore {
	return &TargetStore{path: path}
}

func (s *TargetStore) load() ([]Target, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Target{}, nil
		}
		return nil, err
	}
	list := []Target{}
	if len(data) == 0 {
		return list, nil
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *TargetStore) save(list []Target) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(list)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".deploy-targets-*.tmp")
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

// Find returns the configured Target for (repo, environment), if any.
func (s *TargetStore) Find(repo, environment string) (Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.load()
	if err != nil {
		return Target{}, err
	}
	for _, t := range list {
		if t.Repo == repo && t.Environment == environment {
			return t, nil
		}
	}
	return Target{}, ErrNoTarget
}

// Environments returns every environment name configured for repo, so a
// request form can offer only environments that are actually deployable
// rather than free text.
func (s *TargetStore) Environments(repo string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.load()
	if err != nil {
		return []string{}
	}
	envs := []string{}
	for _, t := range list {
		if t.Repo == repo {
			envs = append(envs, t.Environment)
		}
	}
	return envs
}

// List returns every configured deploy target, for the admin panel's
// management table.
func (s *TargetStore) List() ([]Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Set validates target (see validateTarget) and creates or replaces the
// entry for its (Repo, Environment) key — calling Set again for the same
// pair replaces the previous entry rather than adding a duplicate, the
// same "setting again overwrites" idiom internal/access.Store.Set and
// internal/gittoken.Store.Generate already use. A KeepVersions below 1
// (including the zero value from an omitted field) defaults to 5,
// matching this package's previous NewTargets behavior.
func (s *TargetStore) Set(target Target, allowedSites map[string]bool) error {
	if err := validateTarget(target, allowedSites); err != nil {
		return err
	}
	if target.KeepVersions < 1 {
		target.KeepVersions = 5
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for i, t := range list {
		if t.Repo == target.Repo && t.Environment == target.Environment {
			list[i] = target
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, target)
	}
	return s.save(list)
}

// Delete removes the (repo, environment) target, if any. A target that
// doesn't exist is not an error — matches internal/access.Store.Clear's
// idempotent-remove behavior.
func (s *TargetStore) Delete(repo, environment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, err := s.load()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, t := range list {
		if t.Repo == repo && t.Environment == environment {
			continue
		}
		out = append(out, t)
	}
	return s.save(out)
}
