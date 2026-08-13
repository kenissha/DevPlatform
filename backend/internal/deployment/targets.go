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

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

var ErrNoTarget = errors.New("deployment: no deploy target configured for this repo and environment")

// Target is one (repo, environment) pair this platform is allowed to
// deploy, and exactly how: which build recipe, which IIS site, and which
// relative path inside the release a decrypted secrets file should land
// at (empty means no secrets are injected — not every environment needs
// one, e.g. a test environment with no real credentials).
//
// This is deliberately a fixed, file-loaded list rather than something
// editable from the panel: the design doc's own security note says
// "Build/deploy komutları hiçbir zaman kullanıcıdan gelen serbest metinle
// oluşturulmaz; proje/branch/ortam seçimleri sabit listeden yapılır" — an
// admin edits this file on the server, the same ops-level action
// secretsctl already asks of them, rather than typing a site name into a
// web form that then gets handed to appcmd.exe.
type Target struct {
	Repo          string        `json:"repo"`
	Environment   string        `json:"environment"`
	Recipe        deploy.Recipe `json:"recipe"`
	SiteName      string        `json:"siteName"`
	SecretsTarget string        `json:"secretsTarget,omitempty"`
	KeepVersions  int           `json:"keepVersions"`
}

// Targets is the loaded, immutable set of deployable (repo, environment)
// pairs for this process's lifetime. Re-deploying the server picks up
// file changes; there is no hot-reload, matching how config.Load() itself
// is only read once at startup.
type Targets struct {
	byKey map[string]Target
}

func targetKey(repo, environment string) string {
	return repo + "\x00" + environment
}

// validEnvironmentName mirrors validRepoName: an environment name ends up
// in release directory paths (see deploy.VersionStore) just like a repo
// name does, so it gets the same allow-list rather than a looser one.
var validEnvironmentName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateTargets rejects a targets list that would be unsafe or
// ambiguous to deploy from. It is enforced at load time (see LoadTargets),
// which makes a bad targets file a startup failure rather than something
// discovered at approval time against a live IIS site.
//
// The duplicate check is the important one: (repo, environment) is the key
// that decides which IIS site a deploy overwrites, so two entries claiming
// the same pair are a genuine ambiguity — silently keeping the last one
// means an admin's intended target can be shadowed by an entry they didn't
// notice, with a real site as the blast radius.
func ValidateTargets(list []Target) error {
	seen := make(map[string]struct{}, len(list))
	for i, t := range list {
		switch {
		case !validRepoName.MatchString(t.Repo):
			return fmt.Errorf("deployment: target %d: invalid repo %q", i, t.Repo)
		case !validEnvironmentName.MatchString(t.Environment):
			return fmt.Errorf("deployment: target %d (%s): invalid environment %q", i, t.Repo, t.Environment)
		case t.Recipe != deploy.RecipeDotnet && t.Recipe != deploy.RecipeNpm:
			return fmt.Errorf("deployment: target %d (%s/%s): unknown recipe %q", i, t.Repo, t.Environment, t.Recipe)
		case strings.TrimSpace(t.SiteName) == "":
			return fmt.Errorf("deployment: target %d (%s/%s): siteName is required", i, t.Repo, t.Environment)
		case t.SecretsTarget != "" && !filepath.IsLocal(t.SecretsTarget):
			return fmt.Errorf("deployment: target %d (%s/%s): secretsTarget %q must be a relative path inside the release",
				i, t.Repo, t.Environment, t.SecretsTarget)
		}

		key := targetKey(t.Repo, t.Environment)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("deployment: duplicate deploy target for repo %q environment %q", t.Repo, t.Environment)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// NewTargets builds a Targets from an in-memory list — the programmatic
// counterpart to LoadTargets, used by tests that want to configure a
// target without writing a JSON fixture file. It does not validate: the
// list an admin actually supplies arrives through LoadTargets, which does
// (see ValidateTargets).
func NewTargets(list []Target) *Targets {
	byKey := make(map[string]Target, len(list))
	for _, t := range list {
		if t.KeepVersions < 1 {
			t.KeepVersions = 5 // matches deploydemo's own manually-verified default
		}
		byKey[targetKey(t.Repo, t.Environment)] = t
	}
	return &Targets{byKey: byKey}
}

// LoadTargets reads a JSON array of Target from path. An empty path is
// not an error — it returns an empty Targets, meaning no (repo,
// environment) pair is deployable yet, the safe default until an admin
// deliberately configures one.
func LoadTargets(path string) (*Targets, error) {
	if path == "" {
		return NewTargets(nil), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var list []Target
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	if err := ValidateTargets(list); err != nil {
		return nil, err
	}

	return NewTargets(list), nil
}

// Find returns the configured Target for (repo, environment), if any. A
// nil *Targets (the zero value a caller gets by not wiring one at all)
// behaves like an empty one — Find always returns ErrNoTarget rather than
// panicking, matching the nil-safety already established for
// audit.Logger and notify.Store elsewhere in this codebase.
func (t *Targets) Find(repo, environment string) (Target, error) {
	if t == nil {
		return Target{}, ErrNoTarget
	}
	target, ok := t.byKey[targetKey(repo, environment)]
	if !ok {
		return Target{}, ErrNoTarget
	}
	return target, nil
}

// Environments returns every environment name configured for repo, so a
// request form can offer only environments that are actually deployable
// rather than free text.
func (t *Targets) Environments(repo string) []string {
	if t == nil {
		return []string{}
	}
	envs := []string{}
	for _, target := range t.byKey {
		if target.Repo == repo {
			envs = append(envs, target.Environment)
		}
	}
	return envs
}
