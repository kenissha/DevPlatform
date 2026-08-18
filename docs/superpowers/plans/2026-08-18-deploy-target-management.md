# Deploy Hedeflerinin Panelden Yönetimi Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin add/edit/remove deploy targets (which repo+environment
deploys to which IIS site) from the panel, instead of hand-editing
`DEVPLATFORM_DEPLOY_TARGETS_FILE` and restarting the server.

**Architecture:** Split the deploy-targets file's two jobs apart. The
target *content* (repo, environment, recipe, siteName, secretsTarget,
keepVersions) moves into a new `internal/deployment.TargetStore` — a panel-writable
JSON file under `DataDir`, matching every other Store in this codebase
(`access`, `users`, `gittoken`). Which IIS site *names* are touchable at
all stays a small, ops-only file (`DEVPLATFORM_ALLOWED_SITES_FILE`)
neither the panel nor any API can write to — `internal/iishelper` and
`internal/deployment` both load it independently, the same way both
already independently load the old combined file today.

**Tech Stack:** Go 1.22+ (`net/http`, `encoding/json`), React + TypeScript
(existing `frontend/src` conventions).

**Spec:** `docs/superpowers/specs/2026-08-18-deploy-target-management-design.md`

## Global Constraints

- The allowed-IIS-site-names list is **never** writable through any API
  or panel action — it only changes by editing
  `DEVPLATFORM_ALLOWED_SITES_FILE` on the server and restarting. This is
  the feature's actual security boundary (preserves `iishelper`'s
  original threat model); nothing in this plan may add a way to write to
  that file from `devplatform.exe`.
- No migration of old `DEVPLATFORM_DEPLOY_TARGETS_FILE` data — the spec
  confirms no real target is configured there yet.
- `DEVPLATFORM_DEPLOY_TARGETS_FILE` is fully replaced by
  `DEVPLATFORM_ALLOWED_SITES_FILE` — no backward-compatible transition
  period, matching this project's established practice (git-token
  migration, etc.).
- Both `devplatform.exe` and `iishelper` load the allowed-sites file once
  at startup, independently, with no hot-reload — an ops change to that
  file requires restarting both processes.
- New admin API routes (`/api/deploy-targets`, `/api/allowed-sites`)
  follow the exact `/api/access/{subject}` pattern: `GET` list,
  `PUT .../{key}` create-or-replace, `DELETE .../{key}` remove, all
  behind `auth.RequireRole(auth.RoleAdmin, ...)`.

---

### Task 1: `internal/deployment.TargetStore` (replaces `Targets`)

**Files:**
- Delete: `backend/internal/deployment/targets.go`
- Delete: `backend/internal/deployment/targets_test.go`
- Create: `backend/internal/deployment/store.go`
- Create: `backend/internal/deployment/store_test.go`

**Interfaces:**
- Produces: `deployment.NewTargetStore(path string) *TargetStore`,
  `(*TargetStore).Find(repo, environment string) (Target, error)` (same
  signature `Handlers.Create` already calls),
  `(*TargetStore).Environments(repo string) []string` (same signature
  `Handlers.Environments` already calls), `(*TargetStore).List() ([]Target, error)`,
  `(*TargetStore).Set(target Target, allowedSites map[string]bool) error`,
  `(*TargetStore).Delete(repo, environment string) error`. `Target` struct and
  `ErrNoTarget` are unchanged from the deleted file. Tasks 3 and 4 depend
  on these exact signatures.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/deployment/store_test.go`:

```go
package deployment

import (
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

func TestFind_ReturnsErrNoTargetWhenStoreIsEmpty(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")

	if _, err := store.Find("intranet-backend", "production"); err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
}

func TestSet_ThenFind_ReturnsTheStoredTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"Intranet Backend": true}

	target := Target{
		Repo:          "intranet-backend",
		Environment:   "production",
		Recipe:        deploy.RecipeDotnet,
		SiteName:      "Intranet Backend",
		SecretsTarget: "appsettings.Production.json",
		KeepVersions:  3,
	}
	if err := store.Set(target, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got, err := store.Find("intranet-backend", "production")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if got.SiteName != "Intranet Backend" || got.KeepVersions != 3 || got.SecretsTarget != "appsettings.Production.json" {
		t.Errorf("got %+v, unexpected fields", got)
	}
}

func TestSet_DefaultsKeepVersionsTo5WhenOmitted(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true}

	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	got, err := store.Find("r", "e")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if got.KeepVersions != 5 {
		t.Errorf("KeepVersions = %d, want default of 5", got.KeepVersions)
	}
}

func TestSet_ReplacesAnExistingTargetRatherThanDuplicating(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true}

	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("first Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeDotnet, SiteName: "B", KeepVersions: 9}, allowed); err != nil {
		t.Fatalf("second Set returned error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d targets, want 1 (replaced, not duplicated)", len(list))
	}
	if list[0].Recipe != deploy.RecipeDotnet || list[0].SiteName != "B" || list[0].KeepVersions != 9 {
		t.Errorf("list[0] = %+v, want the replacement's fields", list[0])
	}
}

func TestSet_RejectsASiteNameNotInTheAllowList(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"Approved Site": true}

	err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "Someone Else's Site"}, allowed)
	if err == nil {
		t.Fatal("Set returned no error for a site name outside the allow-list")
	}
	if _, findErr := store.Find("r", "e"); findErr != ErrNoTarget {
		t.Error("expected the rejected target to not be persisted")
	}
}

func TestSet_RejectsInvalidFields(t *testing.T) {
	allowed := map[string]bool{"A": true}
	tests := []struct {
		name   string
		target Target
	}{
		{"empty repo", Target{Repo: "", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"repo escaping its directory", Target{Repo: "../etc", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"empty environment", Target{Repo: "r", Environment: "", Recipe: deploy.RecipeNpm, SiteName: "A"}},
		{"unknown recipe", Target{Repo: "r", Environment: "e", Recipe: "make", SiteName: "A"}},
		{"empty siteName", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: ""}},
		{"secretsTarget escaping the release", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A", SecretsTarget: "../../appsettings.json"}},
		{"absolute secretsTarget", Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A", SecretsTarget: "C:/inetpub/appsettings.json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
			if err := store.Set(tc.target, allowed); err == nil {
				t.Fatal("Set returned no error, want the target rejected")
			}
		})
	}
}

func TestDelete_RemovesTheTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true}
	if err := store.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if err := store.Delete("r", "e"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := store.Find("r", "e"); err != ErrNoTarget {
		t.Fatalf("err = %v, want ErrNoTarget", err)
	}
}

func TestDelete_NonexistentTargetIsNotAnError(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")

	if err := store.Delete("r", "e"); err != nil {
		t.Errorf("Delete on a nonexistent target returned error: %v", err)
	}
}

func TestEnvironments_ReturnsOnlyMatchingRepo(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true, "C": true}
	if err := store.Set(Target{Repo: "intranet-backend", Environment: "production", Recipe: deploy.RecipeDotnet, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "intranet-backend", Environment: "test", Recipe: deploy.RecipeDotnet, SiteName: "B"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "intranet-frontend", Environment: "production", Recipe: deploy.RecipeNpm, SiteName: "C"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	envs := store.Environments("intranet-backend")
	if len(envs) != 2 {
		t.Fatalf("got %d environments, want 2: %v", len(envs), envs)
	}
}

func TestList_ReturnsEveryTarget(t *testing.T) {
	store := NewTargetStore(t.TempDir() + "/deploy-targets.json")
	allowed := map[string]bool{"A": true, "B": true}
	if err := store.Set(Target{Repo: "r1", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set(Target{Repo: "r2", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "B"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d targets, want 2", len(list))
	}
}

func TestTargetStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/deploy-targets.json"
	store1 := NewTargetStore(path)
	allowed := map[string]bool{"A": true}
	if err := store1.Set(Target{Repo: "r", Environment: "e", Recipe: deploy.RecipeNpm, SiteName: "A"}, allowed); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	store2 := NewTargetStore(path)
	if _, err := store2.Find("r", "e"); err != nil {
		t.Errorf("a fresh TargetStore instance backed by the same file does not see the earlier Set: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/deployment/... -run TestFind_ReturnsErrNoTargetWhenStoreIsEmpty -v`
Expected: FAIL — package fails to compile (`NewTargetStore`/`TargetStore`/`Set` undefined; also `targets_test.go` still referencing the soon-to-be-removed `LoadTargets`/`NewTargets`/`ValidateTargets` will fail to compile once `targets.go` is deleted). This is expected — Step 3 deletes the old files and adds the new implementation in the same step.

- [ ] **Step 3: Delete the old files and write the implementation**

Delete `backend/internal/deployment/targets.go` and
`backend/internal/deployment/targets_test.go`.

Create `backend/internal/deployment/store.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/deployment/... -v 2>&1 | head -100`
Expected: the new `store_test.go` tests PASS. `handlers_test.go` and
`handlers.go` will FAIL TO COMPILE at this point — they still reference
the old `*Targets`/`NewTargets` type this step removed. That's expected;
Task 3 fixes them. Confirm specifically that the compile error names
only `handlers.go`/`handlers_test.go` (e.g.
`undefined: Targets`/`undefined: NewTargets`), not `store.go`/`store_test.go`
— if `store_test.go` itself fails to compile or its tests fail, fix
`store.go` before moving on; do not proceed to Task 3 with a broken
Task 1.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/deployment/store.go backend/internal/deployment/store_test.go
git rm backend/internal/deployment/targets.go backend/internal/deployment/targets_test.go
git commit -m "feat(deployment): replace immutable Targets with a panel-writable TargetStore"
```

---

### Task 2: `internal/iishelper` — new allowed-sites file format

**Files:**
- Modify: `backend/internal/iishelper/sites.go`
- Modify: `backend/internal/iishelper/sites_test.go`
- Modify: `backend/cmd/iishelper/main.go`
- Modify: `backend/cmd/iishelper/main_test.go`
- Modify: `backend/cmd/iishelper/install.ps1`

**Interfaces:**
- Consumes: nothing from Task 1 (this task is independent of it).
- Produces: `iishelper.LoadAllowedSites(path string) (map[string]bool, error)`
  — same name and signature as before, but now parses a plain
  `[]string` JSON array instead of `[]targetEntry`. Task 4 calls this
  same function from `cmd/devplatform/main.go`.

- [ ] **Step 1: Update the test to the new format first**

Replace the full contents of `backend/internal/iishelper/sites_test.go`:

```go
package iishelper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllowedSites_EmptyPathReturnsEmptySet(t *testing.T) {
	sites, err := LoadAllowedSites("")
	if err != nil {
		t.Fatalf("expected no error for an empty path, got: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected an empty set, got: %v", sites)
	}
}

func TestLoadAllowedSites_ReadsSiteNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed-sites.json")
	const contents = `["Intranet-F Test", "Intranet-B Test"]`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	sites, err := LoadAllowedSites(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sites["Intranet-F Test"] || !sites["Intranet-B Test"] {
		t.Fatalf("expected both configured site names to be present, got: %v", sites)
	}
	if len(sites) != 2 {
		t.Fatalf("expected exactly 2 sites, got %d: %v", len(sites), sites)
	}
}

func TestLoadAllowedSites_MissingFileIsAnError(t *testing.T) {
	_, err := LoadAllowedSites(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent (but non-empty) path")
	}
}

func TestLoadAllowedSites_MalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowed-sites.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	_, err := LoadAllowedSites(path)
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/iishelper/... -run TestLoadAllowedSites_ReadsSiteNames -v`
Expected: FAIL — `TestLoadAllowedSites_ReadsSiteNames` doesn't match the
old `[]targetEntry`-parsing implementation's expectations yet (the old
test named `TestLoadAllowedSites_ReadsSiteNamesFromTheTargetsFile` is
gone; this new one fails because `LoadAllowedSites` still expects
`{"siteName": ...}` objects, not plain strings).

- [ ] **Step 3: Update the implementation**

Replace the full contents of `backend/internal/iishelper/sites.go`:

```go
package iishelper

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadAllowedSites reads a JSON array of IIS site names from path — the
// only sites this helper will ever agree to repoint. This file is
// deliberately separate from internal/deployment's panel-writable
// target store: it is the actual security boundary (see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
// "Güvenlik" section), so it is edited only by hand on the server,
// pointed at via DEVPLATFORM_ALLOWED_SITES_FILE, and read once at this
// process's startup — never through any API. An empty path returns an
// empty set with no error, matching this codebase's established "no
// file configured means nothing is allowed" safe default.
func LoadAllowedSites(path string) (map[string]bool, error) {
	sites := map[string]bool{}
	if path == "" {
		return sites, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to read allowed sites file %q: %w", path, err)
	}

	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("iishelper: failed to parse allowed sites file %q: %w", path, err)
	}

	for _, name := range names {
		if name != "" {
			sites[name] = true
		}
	}
	return sites, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: PASS (all `TestLoadAllowedSites_*` tests).

- [ ] **Step 5: Update `cmd/iishelper/main.go`'s env var and doc comment**

In `backend/cmd/iishelper/main.go`, change:

```go
// DEVPLATFORM_DEPLOY_TARGETS_FILE is read directly via os.Getenv rather
// than through internal/config.Load(), which would pull in config fields
// (SMTP, JWT secret, etc.) this single-purpose binary has no use for.
//
// DEVPLATFORM_IISHELPER_SDDL is an optional Windows security descriptor
// string restricting which account may connect to the named pipe. Left
// empty, go-winio applies its own default pipe security, which also
// grants Everyone read access — fine for local development, but not
// something production should rely on for restricting access. Production
// should set this explicitly to the one account devplatform.exe runs
// as (see the install script for how to generate this value).
func setup() (net.Listener, *iishelper.Server, error) {
	targetsFile := os.Getenv("DEVPLATFORM_DEPLOY_TARGETS_FILE")
	allowedSites, err := iishelper.LoadAllowedSites(targetsFile)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("iishelper: %d allowed site(s) loaded from %q", len(allowedSites), targetsFile)
```

to:

```go
// DEVPLATFORM_ALLOWED_SITES_FILE is read directly via os.Getenv rather
// than through internal/config.Load(), which would pull in config fields
// (SMTP, JWT secret, etc.) this single-purpose binary has no use for. It
// points at a small, ops-edited JSON array of IIS site names — see
// internal/iishelper.LoadAllowedSites's doc comment for why this file is
// deliberately not the same one internal/deployment's panel-writable
// Store uses.
//
// DEVPLATFORM_IISHELPER_SDDL is an optional Windows security descriptor
// string restricting which account may connect to the named pipe. Left
// empty, go-winio applies its own default pipe security, which also
// grants Everyone read access — fine for local development, but not
// something production should rely on for restricting access. Production
// should set this explicitly to the one account devplatform.exe runs
// as (see the install script for how to generate this value).
func setup() (net.Listener, *iishelper.Server, error) {
	sitesFile := os.Getenv("DEVPLATFORM_ALLOWED_SITES_FILE")
	allowedSites, err := iishelper.LoadAllowedSites(sitesFile)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("iishelper: %d allowed site(s) loaded from %q", len(allowedSites), sitesFile)
```

- [ ] **Step 6: Update `cmd/iishelper/main_test.go`**

In `backend/cmd/iishelper/main_test.go`, change:

```go
	dir := t.TempDir()
	targetsFile := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(targetsFile, []byte(`[{"siteName":"Test Site"}]`), 0o600); err != nil {
		t.Fatalf("failed to write fixture targets file: %v", err)
	}
	t.Setenv("DEVPLATFORM_DEPLOY_TARGETS_FILE", targetsFile)
	t.Setenv("DEVPLATFORM_IISHELPER_SDDL", "")
```

to:

```go
	dir := t.TempDir()
	sitesFile := filepath.Join(dir, "allowed-sites.json")
	if err := os.WriteFile(sitesFile, []byte(`["Test Site"]`), 0o600); err != nil {
		t.Fatalf("failed to write fixture allowed-sites file: %v", err)
	}
	t.Setenv("DEVPLATFORM_ALLOWED_SITES_FILE", sitesFile)
	t.Setenv("DEVPLATFORM_IISHELPER_SDDL", "")
```

- [ ] **Step 7: Update `cmd/iishelper/install.ps1`'s guidance text**

In `backend/cmd/iishelper/install.ps1`, change:

```
Write-Host "The helper also needs DEVPLATFORM_DEPLOY_TARGETS_FILE set to the exact same file"
Write-Host "path devplatform.exe uses. If it is empty or unset, iishelper starts with zero"
Write-Host "allowed sites and rejects every deploy request with 'not a configured deploy"
Write-Host "target site'."
Write-Host ""
Write-Host "IMPORTANT: Windows Services do NOT inherit a logged-in user's environment. Setting"
Write-Host "either DEVPLATFORM_IISHELPER_SDDL or DEVPLATFORM_DEPLOY_TARGETS_FILE with a plain"
Write-Host "user-scoped 'setx' or in a PowerShell profile will NOT be visible to this service."
Write-Host "Both variables must be set as machine-scoped environment variables - System"
Write-Host "Properties > Environment Variables > System variables (not User variables), or:"
Write-Host ""
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_IISHELPER_SDDL', '<value>', 'Machine')"
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_DEPLOY_TARGETS_FILE', '<path>', 'Machine')"
```

to:

```
Write-Host "The helper also needs DEVPLATFORM_ALLOWED_SITES_FILE set to a small JSON file"
Write-Host "listing the IIS site names it may ever touch, e.g. [`"Intranet Backend`", `"Intranet Frontend`"]."
Write-Host "This file is deliberately separate from devplatform.exe's deploy-targets store -"
Write-Host "it is edited by hand on this server only, never through the panel, so a"
Write-Host "devplatform.exe compromise can never expand what iishelper is allowed to touch."
Write-Host "If it is empty or unset, iishelper starts with zero allowed sites and rejects"
Write-Host "every deploy request with 'not a configured deploy target site'."
Write-Host ""
Write-Host "IMPORTANT: Windows Services do NOT inherit a logged-in user's environment. Setting"
Write-Host "either DEVPLATFORM_IISHELPER_SDDL or DEVPLATFORM_ALLOWED_SITES_FILE with a plain"
Write-Host "user-scoped 'setx' or in a PowerShell profile will NOT be visible to this service."
Write-Host "Both variables must be set as machine-scoped environment variables - System"
Write-Host "Properties > Environment Variables > System variables (not User variables), or:"
Write-Host ""
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_IISHELPER_SDDL', '<value>', 'Machine')"
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_ALLOWED_SITES_FILE', '<path>', 'Machine')"
```

- [ ] **Step 8: Build and run the full iishelper package + cmd tests**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/iishelper/... ./cmd/iishelper/... -v`
Expected: builds clean, vets clean, all tests PASS. (`go build ./...`
will still fail elsewhere — `internal/deployment` and `cmd/devplatform`
aren't fixed up until Tasks 3-4 — so if the overall `go build ./...`
reports errors, confirm they are confined to those two packages, not
`internal/iishelper` or `cmd/iishelper`.)

- [ ] **Step 9: Commit**

```bash
git add backend/internal/iishelper/sites.go backend/internal/iishelper/sites_test.go \
        backend/cmd/iishelper/main.go backend/cmd/iishelper/main_test.go \
        backend/cmd/iishelper/install.ps1
git commit -m "feat(iishelper): read allowed sites from their own dedicated file"
```

---

### Task 3: Admin HTTP handlers for deploy-target management

**Files:**
- Modify: `backend/internal/deployment/handlers.go`
- Modify: `backend/internal/deployment/handlers_test.go`
- Create: `backend/internal/deployment/target_handlers.go`
- Create: `backend/internal/deployment/target_handlers_test.go`

**Interfaces:**
- Consumes: `deployment.NewTargetStore`/`Find`/`Environments`/`List`/`Set`/`Delete`,
  `deployment.Target`, `deployment.ErrNoTarget` (Task 1);
  `auth.RequireRole(auth.RoleAdmin, next http.Handler) http.Handler`,
  `auth.UserFromContext` (pre-existing, `backend/internal/auth/auth.go`).
- Produces: `Handlers.Targets` field type changes from `*Targets` to
  `*TargetStore`; new `Handlers.AllowedSites map[string]bool` field; new
  methods `(*Handlers).ListTargets`, `(*Handlers).SetTarget`,
  `(*Handlers).DeleteTarget`, `(*Handlers).ListAllowedSites` — all
  `http.HandlerFunc`-shaped (`func(w http.ResponseWriter, r *http.Request)`).
  Task 4 mounts these as `GET/PUT/DELETE /api/deploy-targets(/{repo}/{environment})`
  and `GET /api/allowed-sites`.

- [ ] **Step 1: Update `Handlers`' struct field and fix existing call sites**

In `backend/internal/deployment/handlers.go`, change:

```go
type Handlers struct {
	Store   *Store
	Repos   *repostore.Store
	Targets *Targets
```

to:

```go
type Handlers struct {
	Store   *Store
	Repos   *repostore.Store
	// Targets is the panel-writable deploy-target store (see
	// internal/deployment/store.go). Find/Environments below are the same
	// methods it always had; Set/Delete/List (new) back the admin
	// management API in target_handlers.go.
	Targets *TargetStore
```

Immediately below, in the same struct, add a new field after `Access`:

```go
	Audit  *audit.Logger
	Notify *notify.Store
	Users  *users.Store
	// Access is optional; a nil Store means every caller sees every repo
	// (see internal/access). ListAll is the only place this package needs
	// it — see taskboard.Handlers.Access's doc comment for why.
	Access *access.Store
	// AllowedSites is the ops-managed set of IIS site names a deploy
	// target's SiteName may name (see internal/iishelper.LoadAllowedSites).
	// Loaded once at startup from DEVPLATFORM_ALLOWED_SITES_FILE — no API
	// here ever writes to it. A nil map behaves as "nothing is allowed"
	// (a Go nil-map read is always the zero value, so every SetTarget call
	// is rejected rather than panicking), matching this codebase's other
	// fail-closed defaults.
	AllowedSites map[string]bool
}
```

`Create` and `Environments` (existing methods, further down in this same
file) already call `h.Targets.Find(...)` and `h.Targets.Environments(...)`
— those calls need no changes, since `*TargetStore` keeps the exact same
method signatures `*Targets` had.

- [ ] **Step 2: Update `newTestHandlers` (git-heavy fixture) to build a `*TargetStore`**

In `backend/internal/deployment/handlers_test.go`, change:

```go
	targets := NewTargets([]Target{
		{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Fake Site"},
	})
```

to:

```go
	targets := NewTargetStore(filepath.Join(dataDir, "deploy-targets.json"))
	if err := targets.Set(
		Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Fake Site"},
		map[string]bool{"Fake Site": true},
	); err != nil {
		t.Fatalf("failed to seed deploy target: %v", err)
	}
```

`h.Targets` is assigned this same `targets` value a few lines below,
unchanged.

- [ ] **Step 3: Add the new routes to the shared test mux**

In `backend/internal/deployment/handlers_test.go`, change:

```go
func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", authMW(http.HandlerFunc(h.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Approve))))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Reject))))
	mux.Handle("GET /api/deployments", authMW(http.HandlerFunc(h.ListAll)))
	return mux
}
```

to:

```go
func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", authMW(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", authMW(http.HandlerFunc(h.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Approve))))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject",
		authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Reject))))
	mux.Handle("GET /api/deployments", authMW(http.HandlerFunc(h.ListAll)))
	mux.Handle("GET /api/deploy-targets", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.ListTargets))))
	mux.Handle("PUT /api/deploy-targets/{repo}/{environment}", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.SetTarget))))
	mux.Handle("DELETE /api/deploy-targets/{repo}/{environment}", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.DeleteTarget))))
	mux.Handle("GET /api/allowed-sites", authMW(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.ListAllowedSites))))
	return mux
}
```

- [ ] **Step 4: Run the existing tests to confirm Steps 1-3 didn't break anything**

Run: `cd backend && go build ./internal/deployment/... 2>&1 | head -50`
Expected: FAIL — `target_handlers.go` doesn't exist yet, so
`h.ListTargets`/`h.SetTarget`/`h.DeleteTarget`/`h.ListAllowedSites` are
undefined. This is expected; Step 5 adds them.

- [ ] **Step 5: Write the failing tests for the new handlers**

Create `backend/internal/deployment/target_handlers_test.go`:

```go
package deployment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

// newDeployTargetsHandlers builds a bare Handlers wired only for
// deploy-target management — no git repo, no Pipeline, unlike
// newTestHandlers, since target CRUD never touches either.
func newDeployTargetsHandlers(t *testing.T) *Handlers {
	t.Helper()
	dataDir := t.TempDir()
	return &Handlers{
		Targets:      NewTargetStore(filepath.Join(dataDir, "deploy-targets.json")),
		AllowedSites: map[string]bool{"Approved Site": true},
	}
}

func TestListTargets_ReturnsEveryConfiguredTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/deploy-targets", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []Target
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "sample" {
		t.Errorf("got %+v, want one target for repo sample", got)
	}
}

func TestSetTarget_CreatesANewTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Approved Site", "keepVersions": 3})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	target, err := h.Targets.Find("sample", "test")
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if target.SiteName != "Approved Site" || target.KeepVersions != 3 {
		t.Errorf("target = %+v, unexpected fields", target)
	}
}

func TestSetTarget_RejectsASiteNotOnTheAllowList(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Someone Else's Site"})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if _, err := h.Targets.Find("sample", "test"); err != ErrNoTarget {
		t.Error("expected the rejected target to not be persisted")
	}
}

func TestSetTarget_ReplacesAnExistingTargetRatherThanDuplicating(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "dotnet", "siteName": "Approved Site", "keepVersions": 9})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	list, err := h.Targets.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 1 || list[0].Recipe != deploy.RecipeDotnet || list[0].KeepVersions != 9 {
		t.Errorf("list = %+v, want exactly one replaced entry", list)
	}
}

func TestSetTarget_RejectsNonAdmin(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]any{"recipe": "npm", "siteName": "Approved Site"})
	req := httptest.NewRequest(http.MethodPut, "/api/deploy-targets/sample/test", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDeleteTarget_RemovesTheTarget(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	if err := h.Targets.Set(Target{Repo: "sample", Environment: "test", Recipe: deploy.RecipeNpm, SiteName: "Approved Site"}, h.AllowedSites); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/deploy-targets/sample/test", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, err := h.Targets.Find("sample", "test"); err != ErrNoTarget {
		t.Error("expected the target to be gone")
	}
}

func TestListAllowedSites_ReturnsTheConfiguredList(t *testing.T) {
	h := newDeployTargetsHandlers(t)
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/allowed-sites", nil)
	req = addAuth(req, t, "admin-1", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 || got[0] != "Approved Site" {
		t.Errorf("got %v, want [\"Approved Site\"]", got)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd backend && go test ./internal/deployment/... -run TestListTargets_ReturnsEveryConfiguredTarget -v`
Expected: FAIL — compile error, `h.ListTargets` (and the other three
new methods) undefined.

- [ ] **Step 7: Write the implementation**

Create `backend/internal/deployment/target_handlers.go`:

```go
package deployment

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

// setTargetRequest is the body PUT /api/deploy-targets/{repo}/{environment}
// expects — everything about a Target except its (repo, environment)
// key, which comes from the URL instead, so the body can never disagree
// with the path about which target is being written.
type setTargetRequest struct {
	Recipe        deploy.Recipe `json:"recipe"`
	SiteName      string        `json:"siteName"`
	SecretsTarget string        `json:"secretsTarget,omitempty"`
	KeepVersions  int           `json:"keepVersions"`
}

// ListTargets handles GET /api/deploy-targets, returning every configured
// deploy target for the admin panel's management table.
func (h *Handlers) ListTargets(w http.ResponseWriter, r *http.Request) {
	list, err := h.Targets.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// SetTarget handles PUT /api/deploy-targets/{repo}/{environment},
// creating or replacing the deploy target for that pair. siteName must
// be one of the server's ops-approved allow-list (h.AllowedSites) — see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
// "Güvenlik" section for why that list is never panel-editable.
func (h *Handlers) SetTarget(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if repo == "" || environment == "" {
		http.Error(w, "400 repo and environment are required", http.StatusBadRequest)
		return
	}

	var req setTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	target := Target{
		Repo:          repo,
		Environment:   environment,
		Recipe:        req.Recipe,
		SiteName:      req.SiteName,
		SecretsTarget: req.SecretsTarget,
		KeepVersions:  req.KeepVersions,
	}
	if err := h.Targets.Set(target, h.AllowedSites); err != nil {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, target)
}

// DeleteTarget handles DELETE /api/deploy-targets/{repo}/{environment}.
func (h *Handlers) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	environment := r.PathValue("environment")
	if repo == "" || environment == "" {
		http.Error(w, "400 repo and environment are required", http.StatusBadRequest)
		return
	}
	if err := h.Targets.Delete(repo, environment); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAllowedSites handles GET /api/allowed-sites — the read-only,
// ops-managed list of IIS site names a deploy target's siteName may
// name. Lets the panel render a dropdown instead of free text; the list
// itself can only be changed by editing DEVPLATFORM_ALLOWED_SITES_FILE
// on the server and restarting, never through this or any other API.
func (h *Handlers) ListAllowedSites(w http.ResponseWriter, r *http.Request) {
	sites := make([]string, 0, len(h.AllowedSites))
	for name := range h.AllowedSites {
		sites = append(sites, name)
	}
	sort.Strings(sites)
	writeJSON(w, http.StatusOK, sites)
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd backend && go test ./internal/deployment/... -v 2>&1 | tail -80`
Expected: PASS — every test in the package, including all of Task 1's,
the pre-existing `TestCreate_*`/`TestApprove_*`/`TestReject_*`/`TestListAll_*`
tests (now compiling against `*TargetStore` instead of `*Targets`), and this
task's new `TestListTargets_*`/`TestSetTarget_*`/`TestDeleteTarget_*`/`TestListAllowedSites_*`.
(`TestApprove_*` tests that exercise real git/build will skip if `git`
isn't on `PATH` — that's expected, matching `requireGit`'s existing
behavior elsewhere in this suite.)

- [ ] **Step 9: Commit**

```bash
git add backend/internal/deployment/handlers.go backend/internal/deployment/handlers_test.go \
        backend/internal/deployment/target_handlers.go backend/internal/deployment/target_handlers_test.go
git commit -m "feat(deployment): add admin HTTP handlers for deploy-target management"
```

---

### Task 4: Wire it all up — config, main.go, server routes

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/devplatform/main.go`
- Modify: `backend/internal/server/server.go`
- Modify: `backend/internal/server/server_test.go`
- Modify: `docs/DURUM.md`

**Interfaces:**
- Consumes: everything produced by Tasks 1-3 (`deployment.NewTargetStore`,
  `deployment.Handlers.AllowedSites`/`ListTargets`/`SetTarget`/`DeleteTarget`/`ListAllowedSites`,
  `iishelper.LoadAllowedSites`).
- Produces: `config.Config.AllowedSitesFile` field; four new routes live
  in `internal/server.NewRouter`. No later task consumes these directly
  — Task 5's frontend calls them over HTTP.

- [ ] **Step 1: Replace `DeployTargetsFile` with `AllowedSitesFile` in config**

In `backend/internal/config/config.go`, change the struct field and its
doc comment:

```go
	// DeployTargetsFile points at a JSON file listing the (repo,
	// environment) pairs this server is allowed to deploy (see
	// deployment.LoadTargets). Empty by default: no target is deployable
	// until an admin deliberately creates this file, matching the design
	// doc's "sabit listeden" requirement — a deploy target is server-side
	// configuration, never something typed into the panel.
	DeployTargetsFile string
```

to:

```go
	// AllowedSitesFile points at a small, ops-edited JSON array of IIS
	// site names (see internal/iishelper.LoadAllowedSites) — the only
	// sites a deploy target's siteName may ever name. Empty by default:
	// no site is approved until an operator deliberately creates this
	// file. Deploy target *content* (which repo/environment maps to
	// which of these sites) is panel-managed (see
	// internal/deployment.TargetStore) — this file is deliberately the one
	// piece that stays outside the panel's reach, see
	// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md's
	// "Güvenlik" section.
	AllowedSitesFile string
```

And in `Load()`'s returned struct literal, change:

```go
		DeployTargetsFile: getEnv("DEVPLATFORM_DEPLOY_TARGETS_FILE", ""),
```

to:

```go
		AllowedSitesFile:  getEnv("DEVPLATFORM_ALLOWED_SITES_FILE", ""),
```

- [ ] **Step 2: Add a config test for the new field**

In `backend/internal/config/config_test.go`, add this test (place it
near `TestLoad_ReadsFrontendDirFromEnv`, whose pattern it mirrors):

```go
func TestLoad_ReadsAllowedSitesFileFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_ALLOWED_SITES_FILE", "C:\\inetpub\\devplatform\\allowed-sites.json")
	defer os.Unsetenv("DEVPLATFORM_ALLOWED_SITES_FILE")

	cfg := Load()

	if cfg.AllowedSitesFile != "C:\\inetpub\\devplatform\\allowed-sites.json" {
		t.Errorf("AllowedSitesFile = %q, want %q", cfg.AllowedSitesFile, "C:\\inetpub\\devplatform\\allowed-sites.json")
	}
}
```

- [ ] **Step 3: Run the config package tests**

Run: `cd backend && go test ./internal/config/... -v`
Expected: PASS — the new test, and every existing test (none of them
reference `DeployTargetsFile`).

- [ ] **Step 4: Wire it up in `main.go`**

In `backend/cmd/devplatform/main.go`, replace:

```go
	targets, err := deployment.LoadTargets(cfg.DeployTargetsFile)
	if err != nil {
		log.Fatalf("failed to load deploy targets from %q: %v", cfg.DeployTargetsFile, err)
	}
	if cfg.DeployTargetsFile == "" {
		log.Printf("no DEVPLATFORM_DEPLOY_TARGETS_FILE configured — deploy requests can be opened but never approved until one is set")
	}
```

with:

```go
	targets := deployment.NewTargetStore(filepath.Join(cfg.DataDir, "deploy-targets.json"))
	allowedSites, err := iishelper.LoadAllowedSites(cfg.AllowedSitesFile)
	if err != nil {
		log.Fatalf("failed to load allowed IIS sites from %q: %v", cfg.AllowedSitesFile, err)
	}
	if cfg.AllowedSitesFile == "" {
		log.Printf("no DEVPLATFORM_ALLOWED_SITES_FILE configured — deploy targets can be viewed but none can ever be saved until an operator approves at least one IIS site")
	} else {
		log.Printf("%d allowed IIS site(s) loaded from %q", len(allowedSites), cfg.AllowedSitesFile)
	}
```

(`iishelper` is already imported in this file — it's used a few lines
below for `iishelper.NewHelperCommandRunner()`.)

Then, in the `deploymentHandlers := &deployment.Handlers{...}` literal
further down, change:

```go
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(cfg.DataDir, "deployments")),
		Repos:        store,
		Targets:      targets,
		Pipeline:     pipeline,
		CheckoutRoot: checkoutRoot,
		Audit:        auditLogger,
		Notify:       notifyStore,
		Users:        usersStore,
		Access:       accessStore,
	}
```

to:

```go
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(cfg.DataDir, "deployments")),
		Repos:        store,
		Targets:      targets,
		Pipeline:     pipeline,
		CheckoutRoot: checkoutRoot,
		Audit:        auditLogger,
		Notify:       notifyStore,
		Users:        usersStore,
		Access:       accessStore,
		AllowedSites: allowedSites,
	}
```

- [ ] **Step 5: Mount the four new routes in `server.go`**

In `backend/internal/server/server.go`, after the existing deploy-request
route block, change:

```go
	mux.Handle("GET /api/repos/{repo}/deploy-targets", repoScoped(http.HandlerFunc(deployments.Environments)))
	mux.Handle("POST /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", repoScoped(http.HandlerFunc(deployments.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve", repoScopedAdmin(http.HandlerFunc(deployments.Approve)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject", repoScopedAdmin(http.HandlerFunc(deployments.Reject)))
	mux.Handle("GET /api/deployments", authMiddleware(http.HandlerFunc(deployments.ListAll)))
```

to:

```go
	mux.Handle("GET /api/repos/{repo}/deploy-targets", repoScoped(http.HandlerFunc(deployments.Environments)))
	mux.Handle("POST /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.Create)))
	mux.Handle("GET /api/repos/{repo}/deployments", repoScoped(http.HandlerFunc(deployments.List)))
	mux.Handle("GET /api/repos/{repo}/deployments/{id}", repoScoped(http.HandlerFunc(deployments.Get)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/approve", repoScopedAdmin(http.HandlerFunc(deployments.Approve)))
	mux.Handle("POST /api/repos/{repo}/deployments/{id}/reject", repoScopedAdmin(http.HandlerFunc(deployments.Reject)))
	mux.Handle("GET /api/deployments", authMiddleware(http.HandlerFunc(deployments.ListAll)))

	// Deploy-target management: entirely Admin-only, not repo-scoped —
	// unlike a deploy request, a target isn't attached to one already-visible
	// repo the caller is proven to see first, so this follows /api/access's
	// pattern (auth.RequireRole directly) rather than repoScopedAdmin's.
	// GET /api/allowed-sites is the read-only, ops-managed site list this
	// API validates siteName against but can never write to — see
	// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md.
	mux.Handle("GET /api/deploy-targets", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.ListTargets))))
	mux.Handle("PUT /api/deploy-targets/{repo}/{environment}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.SetTarget))))
	mux.Handle("DELETE /api/deploy-targets/{repo}/{environment}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.DeleteTarget))))
	mux.Handle("GET /api/allowed-sites", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(deployments.ListAllowedSites))))
```

- [ ] **Step 6: Fix `server_test.go`'s test router and add a router-level admin-only test**

In `backend/internal/server/server_test.go`, change:

```go
	// No Pipeline/Targets wired here: nothing in this file's tests exercises
	// a deploy approval — that full-pipeline path (real checkout, real
	// build, faked-only IIS swap) is covered in internal/deployment's own
	// tests instead.
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(dataDir, "deployments")),
		Repos:        store,
		Targets:      deployment.NewTargets(nil),
		CheckoutRoot: t.TempDir(),
		Audit:        auditLogger,
		Access:       accessStore,
	}
```

to:

```go
	// No Pipeline/Targets wired here: nothing in this file's tests exercises
	// a deploy approval — that full-pipeline path (real checkout, real
	// build, faked-only IIS swap) is covered in internal/deployment's own
	// tests instead.
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(dataDir, "deployments")),
		Repos:        store,
		Targets:      deployment.NewTargetStore(filepath.Join(dataDir, "deploy-targets.json")),
		CheckoutRoot: t.TempDir(),
		Audit:        auditLogger,
		Access:       accessStore,
	}
```

Then, near `TestAccess_ManagementAPIIsAdminOnly` and
`TestGitToken_RevokeIsAdminOnly`, add:

```go
func TestDeployTargets_ManagementAPIIsAdminOnly(t *testing.T) {
	router, _, _ := newTestRouter(t)

	rec := do(t, router, http.MethodGet, "/api/deploy-targets", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/deploy-targets as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodPut, "/api/deploy-targets/sample/test", "dev-1", "developer",
		map[string]any{"recipe": "npm", "siteName": "A"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT /api/deploy-targets/sample/test as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodDelete, "/api/deploy-targets/sample/test", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE /api/deploy-targets/sample/test as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodGet, "/api/allowed-sites", "dev-1", "developer", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/allowed-sites as developer: status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = do(t, router, http.MethodGet, "/api/deploy-targets", "admin-1", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/deploy-targets as admin: status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 7: Build, vet, and run the full backend test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: builds clean, vets clean, all tests PASS. This is the step
that catches any leftover `DeployTargetsFile`/`LoadTargets`/`NewTargets`/`ValidateTargets`
reference Tasks 1-4 missed.

- [ ] **Step 8: Update `docs/DURUM.md`**

In `docs/DURUM.md`, find the "Deploy hedefleri de dosya tabanlı,
panelden yönetilmiyor" bullet inside "Bilinmesi gereken kararlar"
(around where the other resolved-decision bullets like "Çözüldü — git
artık kişi başına anahtarla çalışıyor" live) — it currently reads
approximately:

```markdown
- **Deploy hedefleri de dosya tabanlı, panelden yönetilmiyor.**
  `DEVPLATFORM_DEPLOY_TARGETS_FILE`, `[{repo, environment, recipe,
  siteName, secretsTarget, keepVersions}, ...]` şeklinde bir JSON dosyası.
  Boşsa/yoksa hiçbir repo deploy edilemez — güvenli varsayılan.
```

Replace it with:

```markdown
- **Çözüldü — deploy hedefleri artık panelden yönetiliyor (2026-08-18):**
  Eski tek dosyanın iki işi ayrıldı. Hedefin içeriği (repo, environment,
  recipe, siteName, secretsTarget, keepVersions) artık
  `internal/deployment.TargetStore` diye panelden CRUD edilen bir depoda
  (`DataDir/deploy-targets.json`) — yeni "Deploy Hedefleri" admin
  sayfası, `GET/PUT/DELETE /api/deploy-targets(/{repo}/{environment})`.
  Hangi IIS site adlarına dokunulabileceği ise hâlâ sadece sunucuya elle
  yazılan, küçük, ayrı bir dosyada (`DEVPLATFORM_ALLOWED_SITES_FILE`,
  eski `DEVPLATFORM_DEPLOY_TARGETS_FILE`'ın yerine geçti) — panel bu
  listeye asla yazamaz, sadece `GET /api/allowed-sites` ile okuyup bir
  dropdown'da gösterir. Bu ayrım kasıtlı: `iishelper`'ın var oluş
  sebebini (devplatform.exe ele geçirilse bile appcmd'nin sadece
  önceden onaylı site'lara dokunabilmesi) koruyor. Ayrıntı için
  `docs/superpowers/specs/2026-08-18-deploy-target-management-design.md`.
  Site listesi değiştiğinde (yeni bir IIS site'ı elle açıldığında) hem
  `iishelper` hem `devplatform.exe` yeniden başlatılmalı — ikisi de bu
  dosyayı sadece süreç başlarken bir kere okuyor.
```

- [ ] **Step 9: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go \
        backend/cmd/devplatform/main.go backend/internal/server/server.go \
        backend/internal/server/server_test.go docs/DURUM.md
git commit -m "feat(deployment): wire panel-managed deploy targets into main.go and routes"
```

---

### Task 5: Frontend — "Deploy Hedefleri" admin page

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/pages/DeployTargetsPage.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/AppLayout.tsx`

**Interfaces:**
- Consumes: `GET/PUT/DELETE /api/deploy-targets(/{repo}/{environment})`,
  `GET /api/allowed-sites` (Task 4); `useRepos()` from
  `frontend/src/repos/ReposContext` (existing, returns
  `{ repos: string[] | null, ... }`); `DeployIcon` from
  `frontend/src/components/icons` (existing, already used for the
  per-repo "Deploy" nav link).
- Produces: `DeployTarget` type (`frontend/src/api/types.ts`);
  `api.listDeployTargets()`, `api.setDeployTarget(repo, environment, target)`,
  `api.deleteDeployTarget(repo, environment)`, `api.listAllowedSites()`
  (`frontend/src/api/client.ts`). No later task consumes these — this is
  the last task in the plan.

- [ ] **Step 1: Add the `DeployTarget` type**

In `frontend/src/api/types.ts`, after the existing `DeploymentRequest`
interface, add:

```ts
export type DeployRecipe = 'dotnet' | 'npm'

// One (repo, environment) pair's deploy configuration — mirrors
// backend/internal/deployment.Target. Managed from the panel's "Deploy
// Hedefleri" page (Admin-only); siteName must be one of
// GET /api/allowed-sites's values, enforced server-side.
export interface DeployTarget {
  repo: string
  environment: string
  recipe: DeployRecipe
  siteName: string
  secretsTarget?: string
  keepVersions: number
}
```

- [ ] **Step 2: Add the API client methods**

In `frontend/src/api/client.ts`, add `DeployRecipe` and `DeployTarget`
to the type-only import block at the top:

```ts
import type {
  AccessRegistry,
  AuditEvent,
  Commit,
  Contributor,
  DayCount,
  DeployRecipe,
  DeployTarget,
  DeploymentRequest,
  DeploymentStatus,
  DiffResult,
  MergeRequest,
  MergeRequestDetail,
  MergeRequestStatus,
  Notification,
  Person,
  Task,
  TaskStatus,
  User,
} from './types'
```

Then, inside the `api` object, change:

```ts
  listAllDeployments: (status?: DeploymentStatus) =>
    request<DeploymentRequest[]>(`/api/deployments${status ? `?status=${status}` : ''}`),

  // Per-project authorization (Admin-only on the backend). A subject
```

to:

```ts
  listAllDeployments: (status?: DeploymentStatus) =>
    request<DeploymentRequest[]>(`/api/deployments${status ? `?status=${status}` : ''}`),

  // Deploy-target management (Admin-only on the backend). siteName is
  // validated server-side against the ops-managed allow-list — see
  // listAllowedSites — never accepted as free text.
  listDeployTargets: () => request<DeployTarget[]>('/api/deploy-targets'),
  setDeployTarget: (repo: string, environment: string, target: Omit<DeployTarget, 'repo' | 'environment'>) =>
    request<DeployTarget>(
      `/api/deploy-targets/${encodeURIComponent(repo)}/${encodeURIComponent(environment)}`,
      { method: 'PUT', body: JSON.stringify(target) },
    ),
  deleteDeployTarget: (repo: string, environment: string) =>
    request<void>(
      `/api/deploy-targets/${encodeURIComponent(repo)}/${encodeURIComponent(environment)}`,
      { method: 'DELETE' },
    ),
  listAllowedSites: () => request<string[]>('/api/allowed-sites'),

  // Per-project authorization (Admin-only on the backend). A subject
```

Finally, add `DeployRecipe` and `DeployTarget` to the
`export type { ... }` block at the bottom of the file:

```ts
export type {
  AccessRegistry,
  AuditEvent,
  Commit,
  Contributor,
  DayCount,
  DeployRecipe,
  DeployTarget,
  DeploymentRequest,
  DiffResult,
  MergeRequest,
  MergeRequestDetail,
  Notification,
  Person,
  Task,
  User,
}
```

- [ ] **Step 3: Create the page**

Create `frontend/src/pages/DeployTargetsPage.tsx`:

```tsx
import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type DeployRecipe, type DeployTarget } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { DeployIcon } from '../components/icons'
import { useRepos } from '../repos/ReposContext'

// DeployTargetsPage is the admin-only screen for managing which
// (repo, environment) pair deploys to which IIS site — see
// docs/superpowers/specs/2026-08-18-deploy-target-management-design.md.
// siteName is always chosen from GET /api/allowed-sites, never typed:
// that list is ops-managed and the one thing this page can never write
// to, which is the feature's actual security boundary.
export function DeployTargetsPage() {
  const { user } = useAuth()
  const { repos } = useRepos()
  const [targets, setTargets] = useState<DeployTarget[] | null>(null)
  const [allowedSites, setAllowedSites] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  function reload() {
    Promise.all([api.listDeployTargets(), api.listAllowedSites()])
      .then(([t, sites]) => {
        setTargets(t)
        setAllowedSites(sites)
        setError(null)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Deploy hedefleri yüklenemedi'))
  }

  useEffect(reload, [])

  if (user?.role !== 'admin') {
    return (
      <div className="page">
        <p className="error">Bu sayfa sadece yöneticiler içindir.</p>
      </div>
    )
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Deploy hedefleri</h1>
          <p className="page-subtitle">Hangi repo hangi ortama, hangi IIS site'ına deploy olur</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <div className="card">
        {targets === null && <p className="empty-state">Yükleniyor...</p>}
        {targets?.length === 0 && <p className="empty-state">Henüz deploy hedefi yok.</p>}
        {targets && targets.length > 0 && (
          <ul className="row-list">
            {targets.map((t) => (
              <li key={`${t.repo}/${t.environment}`}>
                <div className="row-main">
                  <DeployIcon className="muted" />
                  <span className="row-title">
                    {t.repo} → {t.environment}
                  </span>
                  <span className="spacer" />
                  <span className="badge badge-neutral">{t.recipe}</span>
                  <span className="badge badge-neutral">{t.siteName}</span>
                  <button
                    type="button"
                    className="btn-ghost"
                    onClick={async () => {
                      try {
                        await api.deleteDeployTarget(t.repo, t.environment)
                        reload()
                      } catch (err) {
                        setError(err instanceof ApiError ? err.message : 'Silinemedi')
                      }
                    }}
                  >
                    Sil
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni deploy hedefi</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <NewTargetForm repos={repos ?? []} allowedSites={allowedSites ?? []} onCreated={reload} />
        </div>
      </div>
    </div>
  )
}

function NewTargetForm({
  repos,
  allowedSites,
  onCreated,
}: {
  repos: string[]
  allowedSites: string[]
  onCreated: () => void
}) {
  const [repo, setRepo] = useState('')
  const [environment, setEnvironment] = useState('')
  const [recipe, setRecipe] = useState<DeployRecipe>('dotnet')
  const [siteName, setSiteName] = useState('')
  const [secretsTarget, setSecretsTarget] = useState('')
  const [keepVersions, setKeepVersions] = useState(5)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!repo || !environment.trim() || !siteName) return
    setSaving(true)
    setFormError(null)
    try {
      await api.setDeployTarget(repo, environment.trim(), {
        recipe,
        siteName,
        secretsTarget: secretsTarget.trim() || undefined,
        keepVersions,
      })
      setEnvironment('')
      setSecretsTarget('')
      onCreated()
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="stacked-form">
      <div className="field">
        <label htmlFor="target-repo">Repo</label>
        <select id="target-repo" value={repo} onChange={(e) => setRepo(e.target.value)}>
          <option value="">Seçin...</option>
          {repos.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </div>

      <div className="field">
        <label htmlFor="target-environment">Ortam</label>
        <input
          id="target-environment"
          type="text"
          value={environment}
          onChange={(e) => setEnvironment(e.target.value)}
          placeholder="production"
        />
      </div>

      <div className="field">
        <label htmlFor="target-recipe">Recipe</label>
        <select id="target-recipe" value={recipe} onChange={(e) => setRecipe(e.target.value as DeployRecipe)}>
          <option value="dotnet">dotnet</option>
          <option value="npm">npm</option>
        </select>
      </div>

      <div className="field">
        <label htmlFor="target-site">IIS site'ı</label>
        <select id="target-site" value={siteName} onChange={(e) => setSiteName(e.target.value)}>
          <option value="">Seçin...</option>
          {allowedSites.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        {allowedSites.length === 0 && (
          <p className="empty-state">Onaylı IIS site'ı yok — sunucuda DEVPLATFORM_ALLOWED_SITES_FILE ayarlanmalı.</p>
        )}
      </div>

      <div className="field">
        <label htmlFor="target-secrets">Secrets hedefi (opsiyonel)</label>
        <input
          id="target-secrets"
          type="text"
          value={secretsTarget}
          onChange={(e) => setSecretsTarget(e.target.value)}
          placeholder="appsettings.Production.json"
        />
      </div>

      <div className="field">
        <label htmlFor="target-keep">Saklanacak sürüm sayısı</label>
        <input
          id="target-keep"
          type="number"
          min={1}
          value={keepVersions}
          onChange={(e) => setKeepVersions(Number(e.target.value) || 1)}
        />
      </div>

      <div className="form-actions">
        <button type="submit" className="btn-primary" disabled={saving || !repo || !environment.trim() || !siteName}>
          {saving ? 'Kaydediliyor...' : 'Kaydet'}
        </button>
      </div>
      {formError && <p className="error">{formError}</p>}
    </form>
  )
}
```

- [ ] **Step 4: Wire the route**

In `frontend/src/App.tsx`, add the import after `DashboardPage`'s
(alphabetical: `DashboardPage` < `DeployTargetsPage` < `HesabimPage`):

```ts
import { DashboardPage } from './pages/DashboardPage'
import { DeployTargetsPage } from './pages/DeployTargetsPage'
import { HesabimPage } from './pages/HesabimPage'
```

Add the route after `/access`:

```tsx
              <Route path="/access" element={<AccessPage />} />
              <Route path="/deploy-targets" element={<DeployTargetsPage />} />
              <Route path="/hesabim" element={<HesabimPage />} />
```

- [ ] **Step 5: Add the nav link**

In `frontend/src/components/AppLayout.tsx`, `DeployIcon` is already
imported (used by the per-repo "Deploy" link) — no import change needed.
Add the nav item next to the existing admin-only "Proje erişimi" item:

```tsx
              {user?.role === 'admin' && (
                <li>
                  <NavLink end to="/access" className={navClass}>
                    <LockIcon />
                    <span className="nav-label">Proje erişimi</span>
                  </NavLink>
                </li>
              )}
              {user?.role === 'admin' && (
                <li>
                  <NavLink end to="/deploy-targets" className={navClass}>
                    <DeployIcon />
                    <span className="nav-label">Deploy hedefleri</span>
                  </NavLink>
                </li>
              )}
```

- [ ] **Step 6: Build and lint**

Run: `cd frontend && npm run build && npm run lint`
Expected: build succeeds, lint clean (catches unused-import/type errors
across Steps 1-5).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts \
        frontend/src/pages/DeployTargetsPage.tsx frontend/src/App.tsx \
        frontend/src/components/AppLayout.tsx
git commit -m "feat(frontend): add Deploy Hedefleri admin page for panel-managed deploy targets"
```
