# Deploy Automation Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the core Faz 2 mechanism — build a project, write the output to a new versioned folder, and swap an IIS site's physical path to it — end to end, against a throwaway test IIS site on this machine, with zero connection to the real Intranet or any secrets. This is the foundation later plans (secrets injection, the approval-gated request workflow, per-repo admin configuration) build on.

**Architecture:** A new `internal/deploy` package with three independently-testable pieces: `Builder` (runs a fixed-recipe build command — `dotnet publish` or `npm run build` — as a subprocess with fixed argv, never a user-supplied string), `VersionStore` (creates/lists/prunes versioned release folders on disk, file-based like every other store in this codebase), and `IISSwapper` (wraps `appcmd.exe` to point a site at a folder). `IISSwapper`'s command execution is injected behind a small interface so its argument-building logic is fully unit-testable without ever invoking the real `appcmd.exe` — real `appcmd` execution requires Administrator privileges this development environment's automated tooling doesn't have, so that specific step is verified manually, together, in Task 4.

**Tech Stack:** Go 1.22+ (backend, matching every existing internal package). No new external Go dependencies — `os/exec` for subprocesses, standard library only. Test fixtures use the `dotnet` CLI (SDK 10.0.301, confirmed installed) and `npm`/`node` (confirmed installed) directly — no mocked build tools, real builds of tiny fixture projects.

## Global Constraints

- **No free text in any command.** Every `exec.Command` call in this plan has a fixed program name and a fixed, small set of arguments built from validated inputs (repo names matching the existing `^[a-zA-Z0-9_-]+$` pattern used throughout this codebase, or enum-typed recipe/environment values) — never string concatenation of user input into a shell command, and never a shell (`cmd /c`, `sh -c`) invocation at all; always direct `exec.Command(program, arg1, arg2, ...)`.
- **This plan does not touch the real Intranet-F/Intranet-B projects or any real IIS site.** Every test and manual verification step in this plan targets a throwaway site/project created for this purpose. Connecting to the real projects is a deliberate, separate, future step — not something that happens by default or by omission.
- **This plan does not implement secrets injection.** The design doc's Faz 2 secrets-store step (copying real `appsettings.Production.json` into the versioned folder before swap) is out of scope here — deferred to a follow-up plan. Nothing in this plan reads or writes anything resembling a real credential.
- **`appcmd.exe` execution needs Administrator privileges** that this development session's tools don't have (confirmed empirically during planning — even `appcmd list site` failed with a permissions error under a non-elevated shell). `IISSwapper`'s tests must NOT attempt to invoke the real `appcmd.exe`; they test argument construction against an injected fake executor. Real `appcmd` execution is verified once, manually, in Task 4, with the project owner running the actual command in an elevated PowerShell window and reporting the result back.
- Commit after every task; each commit must leave `go build ./...`, `go vet ./...`, and `go test ./...` (from `backend/`) passing.
- All code comments in English; commit messages Conventional-Commits-ish (`feat:`/`test:`/`fix:`), in English.
- Follow this codebase's established conventions: sentinel errors checked with `errors.Is`, `0o750`/`0o640` file permissions, atomic writes via temp-file-then-rename where a file is rewritten in place (matching `internal/users`), one clear responsibility per file.

---

### Task 1: `internal/deploy` — Build recipes (`dotnet`, `npm`)

**Files:**
- Create: `backend/internal/deploy/build.go`
- Test: `backend/internal/deploy/build_test.go`
- Create: `backend/internal/deploy/testdata/dotnet-fixture/Fixture.csproj`
- Create: `backend/internal/deploy/testdata/dotnet-fixture/Program.cs`
- Create: `backend/internal/deploy/testdata/npm-fixture/package.json`
- Create: `backend/internal/deploy/testdata/npm-fixture/build.js`

**Interfaces:**
- Consumes: nothing from other new-this-plan code.
- Produces: `deploy.Recipe` (a string-based enum type: `RecipeDotnet`, `RecipeNpm`), `deploy.Builder` with `Build(sourceDir string, recipe Recipe, outputDir string) error`. Task 4 calls this directly.

- [ ] **Step 1: Create the dotnet test fixture**

A minimal, dependency-free console app so `dotnet publish` works fully offline (no NuGet restore needed beyond what's already cached with the SDK).

Create `backend/internal/deploy/testdata/dotnet-fixture/Fixture.csproj`:
```xml
<Project Sdk="Microsoft.NET.Sdk">

  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>enable</Nullable>
  </PropertyGroup>

</Project>
```

Create `backend/internal/deploy/testdata/dotnet-fixture/Program.cs`:
```csharp
Console.WriteLine("deploy fixture build ok");
```

Verify this fixture actually builds before writing any Go code, so a later test failure can't be confused with a fixture problem:
```bash
cd backend/internal/deploy/testdata/dotnet-fixture
dotnet publish . -o /tmp/deploy-fixture-check -c Release
```
Expected: succeeds, producing files in the output directory including `Fixture.dll`. If your SDK version differs from `net10.0` and this fails with a framework-not-found error, adjust `<TargetFramework>` to match `dotnet --list-sdks`' major version (e.g. `net8.0`) — note which version you used in your report. Delete the `/tmp/deploy-fixture-check` directory afterward, it was just a manual check.

- [ ] **Step 2: Create the npm test fixture**

A minimal build script with no real bundler dependency, so `npm install` isn't even needed — `npm run build` just runs a plain Node script.

Create `backend/internal/deploy/testdata/npm-fixture/package.json`:
```json
{
  "name": "deploy-fixture",
  "private": true,
  "scripts": {
    "build": "node build.js"
  }
}
```

Create `backend/internal/deploy/testdata/npm-fixture/build.js`:
```js
const fs = require('fs');
const path = require('path');

const distDir = path.join(__dirname, 'dist');
fs.mkdirSync(distDir, { recursive: true });
fs.writeFileSync(path.join(distDir, 'index.html'), '<html><body>deploy fixture build ok</body></html>\n');
```

Verify: `cd backend/internal/deploy/testdata/npm-fixture && npm run build` — expected: creates `dist/index.html`. Delete the generated `dist/` folder afterward (it's a build artifact, not a fixture — Step 4 below adds a `.gitignore` entry so this doesn't need repeating by hand every time, but clean up this first manual check yourself).

- [ ] **Step 3: Write the failing tests**

Create `backend/internal/deploy/build_test.go`:
```go
package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found on PATH, skipping", name)
	}
}

func TestBuild_Dotnet_ProducesOutput(t *testing.T) {
	requireTool(t, "dotnet")

	source, err := filepath.Abs("testdata/dotnet-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	outputDir := t.TempDir()

	b := &Builder{}
	if err := b.Build(source, RecipeDotnet, outputDir); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "Fixture.dll")); err != nil {
		t.Errorf("expected Fixture.dll in output dir: %v", err)
	}
}

func TestBuild_Npm_ProducesOutput(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	outputDir := t.TempDir()

	b := &Builder{}
	if err := b.Build(source, RecipeNpm, outputDir); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outputDir, "index.html"))
	if err != nil {
		t.Fatalf("expected index.html in output dir: %v", err)
	}
	if string(content) == "" {
		t.Error("index.html is empty")
	}
}

func TestBuild_RejectsUnknownRecipe(t *testing.T) {
	b := &Builder{}
	err := b.Build(t.TempDir(), Recipe("not-a-real-recipe"), t.TempDir())
	if err == nil {
		t.Fatal("expected an error for an unknown recipe, got nil")
	}
}
```

- [ ] **Step 4: Run to verify they fail**

Run: `go test ./internal/deploy/... -v` from `backend/`.
Expected: FAIL — package `deploy` doesn't exist yet.

- [ ] **Step 5: Implement `build.go`**

Create `backend/internal/deploy/build.go`:
```go
// Package deploy builds and releases projects hosted on this platform. It
// deliberately never accepts a free-text build command from a caller —
// every build is one of a small, fixed set of recipes, so nothing a user
// types ever reaches a shell.
package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Recipe identifies a fixed, known way to build a project. There is no
// "custom command" recipe — adding a new project type means adding a new
// Recipe constant and a case in Builder.Build, not accepting a string from
// a caller.
type Recipe string

const (
	RecipeDotnet Recipe = "dotnet"
	RecipeNpm    Recipe = "npm"
)

// Builder runs a fixed build recipe against sourceDir, writing the result
// to outputDir. outputDir is created if it doesn't exist; it is the
// caller's responsibility to pass a fresh, empty directory (see
// VersionStore in a later task) — Builder does not clean up outputDir
// itself.
type Builder struct{}

// Build runs recipe against sourceDir and writes the result to outputDir.
// Every exec.Command call here uses a fixed program name and a fixed
// argument list built only from sourceDir/outputDir (paths the caller
// controls, not free text from an end user) — never a shell, never string
// concatenation into a command line.
func (b *Builder) Build(sourceDir string, recipe Recipe, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("deploy: failed to create output dir: %w", err)
	}

	switch recipe {
	case RecipeDotnet:
		return b.buildDotnet(sourceDir, outputDir)
	case RecipeNpm:
		return b.buildNpm(sourceDir, outputDir)
	default:
		return fmt.Errorf("deploy: unknown recipe %q", recipe)
	}
}

func (b *Builder) buildDotnet(sourceDir, outputDir string) error {
	cmd := exec.Command("dotnet", "publish", sourceDir, "-o", outputDir, "-c", "Release")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploy: dotnet publish failed: %w\n%s", err, out)
	}
	return nil
}

func (b *Builder) buildNpm(sourceDir, outputDir string) error {
	cmd := exec.Command("npm", "run", "build")
	cmd.Dir = sourceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("deploy: npm run build failed: %w\n%s", err, out)
	}

	// npm's convention is to write build output to a "dist" folder inside
	// sourceDir; copy its contents into outputDir so every recipe has the
	// same "outputDir now holds the deployable artifact" contract.
	return copyDir(filepath.Join(sourceDir, "dist"), outputDir)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("deploy: failed to read npm build output %q: %w", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // fixture/test scope: flat output only, no nested dirs to copy
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o640); err != nil {
			return err
		}
	}
	return nil
}
```

Note: `copyDir`'s "flat output only" limitation is intentional and narrow for this proof-of-concept plan — real frontend builds produce nested `assets/` subdirectories. A later plan (once this mechanism is proven and connected to a real project) should extend `copyDir` to walk subdirectories recursively; don't over-build that here since the npm fixture in this task is deliberately flat and nothing in this plan needs more.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, all 3 tests (`TestBuild_Dotnet_ProducesOutput`, `TestBuild_Npm_ProducesOutput`, `TestBuild_RejectsUnknownRecipe`).

- [ ] **Step 7: Add a .gitignore entry for the npm fixture's build output**

Add to the repo's root `.gitignore` (it already has a `frontend/dist/` line — add a sibling for the fixture, which is a different directory):
```
backend/internal/deploy/testdata/npm-fixture/dist/
```

- [ ] **Step 8: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/deploy/build.go backend/internal/deploy/build_test.go backend/internal/deploy/testdata .gitignore
git commit -m "feat: build.Recipe and Builder for fixed dotnet/npm build recipes"
```

---

### Task 2: `internal/deploy` — VersionStore (versioned release folders)

**Files:**
- Create: `backend/internal/deploy/versionstore.go`
- Test: `backend/internal/deploy/versionstore_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 directly (standalone, filesystem-only).
- Produces: `deploy.VersionStore` with `NewVersionStore(rootDir string) *VersionStore`, `(*VersionStore) NewRelease(repo, environment string) (releaseDir string, err error)`, `(*VersionStore) List(repo, environment string) ([]string, error)` (release directory paths, newest first), `(*VersionStore) Prune(repo, environment string, keep int) error`. Task 4 calls `NewRelease` to get a fresh directory to pass as `Builder.Build`'s `outputDir`, and `Prune` after a successful deploy.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/deploy/versionstore_test.go`:
```go
package deploy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRelease_CreatesAFreshEmptyDirectory(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	dir, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("NewRelease returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected release dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected release dir to be a directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read release dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected a fresh empty dir, got %d entries", len(entries))
	}
}

func TestNewRelease_SuccessiveCallsGetDifferentDirectories(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	first, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("first NewRelease failed: %v", err)
	}
	// Ensure a distinguishable timestamp even on a fast filesystem/clock.
	time.Sleep(2 * time.Second)
	second, err := vs.NewRelease("sample", "test")
	if err != nil {
		t.Fatalf("second NewRelease failed: %v", err)
	}

	if first == second {
		t.Fatalf("expected distinct release directories, both were %q", first)
	}
}

func TestList_ReturnsReleasesNewestFirst(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	first, _ := vs.NewRelease("sample", "test")
	time.Sleep(2 * time.Second)
	second, _ := vs.NewRelease("sample", "test")

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2", len(releases))
	}
	if releases[0] != second || releases[1] != first {
		t.Errorf("releases = %v, want newest (%q) first then %q", releases, second, first)
	}
}

func TestList_ReturnsEmptySliceWhenNoReleasesYet(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("got %d releases, want 0", len(releases))
	}
}

func TestPrune_KeepsOnlyTheNewestN(t *testing.T) {
	vs := NewVersionStore(t.TempDir())

	var made []string
	for i := 0; i < 4; i++ {
		dir, err := vs.NewRelease("sample", "test")
		if err != nil {
			t.Fatalf("NewRelease #%d failed: %v", i, err)
		}
		made = append(made, dir)
		time.Sleep(2 * time.Second)
	}

	if err := vs.Prune("sample", "test", 2); err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases after pruning to 2, want 2", len(releases))
	}
	// The two newest (made[3], made[2]) must survive; the two oldest must be gone from disk.
	for _, old := range made[:2] {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Errorf("expected pruned release dir %q to be deleted from disk", old)
		}
	}
}

func TestNewRelease_RejectsInvalidRepo(t *testing.T) {
	vs := NewVersionStore(t.TempDir())
	if _, err := vs.NewRelease("../escape", "test"); err == nil {
		t.Fatal("expected an error for a path-traversal repo name")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/deploy/... -run TestNewRelease -v` and similarly for `TestList`/`TestPrune`.
Expected: FAIL — `VersionStore`/`NewVersionStore` don't exist yet.

- [ ] **Step 3: Implement `versionstore.go`**

Create `backend/internal/deploy/versionstore.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, all 6 new tests plus Task 1's 3 tests still passing.

- [ ] **Step 5: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/deploy/versionstore.go backend/internal/deploy/versionstore_test.go
git commit -m "feat: VersionStore for versioned release directories with pruning"
```

---

### Task 3: `internal/deploy` — IISSwapper (appcmd wrapper, injectable executor)

**Files:**
- Create: `backend/internal/deploy/iisswap.go`
- Test: `backend/internal/deploy/iisswap_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1-2.
- Produces: `deploy.CommandRunner` interface (`Run(name string, args ...string) (output []byte, err error)`), `deploy.RealCommandRunner` (the production implementation, calling `exec.Command` for real), `deploy.IISSwapper` with `NewIISSwapper(runner CommandRunner) *IISSwapper` and `(*IISSwapper) SetPhysicalPath(siteName, path string) error`. Task 4 constructs `NewIISSwapper(&RealCommandRunner{})` for real use, and its own tests use a fake `CommandRunner`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/deploy/iisswap_test.go`:
```go
package deploy

import (
	"errors"
	"testing"
)

// fakeCommandRunner records every call it receives instead of executing
// anything real — this is how IISSwapper's argument-building logic gets
// tested without ever invoking the real appcmd.exe, which requires
// Administrator privileges this test environment doesn't have.
type fakeCommandRunner struct {
	calls   [][]string
	failWith error
}

func (f *fakeCommandRunner) Run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.failWith != nil {
		return nil, f.failWith
	}
	return []byte("ok"), nil
}

func TestSetPhysicalPath_InvokesAppcmdWithExpectedArguments(t *testing.T) {
	runner := &fakeCommandRunner{}
	swapper := NewIISSwapper(runner)

	if err := swapper.SetPhysicalPath("DevPlatform Test Site", `C:\releases\sample\test\20260812T000000.000000000`); err != nil {
		t.Fatalf("SetPhysicalPath returned error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d calls to the command runner, want 1", len(runner.calls))
	}
	got := runner.calls[0]
	want := []string{
		"appcmd", "set", "vdir", "DevPlatform Test Site/",
		`/physicalPath:C:\releases\sample\test\20260812T000000.000000000`,
	}
	if len(got) != len(want) {
		t.Fatalf("call = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSetPhysicalPath_PropagatesCommandRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{failWith: errors.New("appcmd exited 5: access denied")}
	swapper := NewIISSwapper(runner)

	err := swapper.SetPhysicalPath("DevPlatform Test Site", `C:\releases\sample\test\v1`)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/deploy/... -run TestSetPhysicalPath -v`
Expected: FAIL — `IISSwapper`/`NewIISSwapper` don't exist yet.

- [ ] **Step 3: Implement `iisswap.go`**

Create `backend/internal/deploy/iisswap.go`:
```go
package deploy

import (
	"fmt"
	"os/exec"
)

// CommandRunner executes a named program with fixed arguments and returns
// its combined output. Abstracted behind an interface so IISSwapper's
// argument-building logic can be tested without invoking the real
// appcmd.exe, which requires Administrator privileges.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// RealCommandRunner is the production CommandRunner: it actually executes
// the given program via os/exec, never a shell.
type RealCommandRunner struct{}

func (RealCommandRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("deploy: %s %v failed: %w\n%s", name, args, err, out)
	}
	return out, nil
}

// IISSwapper points an IIS site at a new physical path via appcmd.exe.
type IISSwapper struct {
	runner CommandRunner
}

// NewIISSwapper returns an IISSwapper that executes commands via runner.
// Production callers pass &RealCommandRunner{}; tests pass a fake.
func NewIISSwapper(runner CommandRunner) *IISSwapper {
	return &IISSwapper{runner: runner}
}

// SetPhysicalPath points siteName's root virtual directory at path. Both
// arguments must already be validated/trusted by the caller (e.g.
// siteName resolved from a fixed admin-configured mapping, path built by
// VersionStore) — this function does not itself validate them beyond
// passing them as separate argv entries (never concatenated into a single
// string), which is what actually matters for command-injection safety:
// appcmd receives siteName and the physicalPath value as distinct,
// unparsed arguments, exactly as os/exec passes them to the OS, with no
// shell in between to reinterpret special characters.
func (s *IISSwapper) SetPhysicalPath(siteName, path string) error {
	_, err := s.runner.Run("appcmd", "set", "vdir", siteName+"/", "/physicalPath:"+path)
	if err != nil {
		return fmt.Errorf("deploy: failed to set physical path for site %q: %w", siteName, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, all tests across Task 1-3.

- [ ] **Step 5: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/deploy/iisswap.go backend/internal/deploy/iisswap_test.go
git commit -m "feat: IISSwapper wrapping appcmd.exe behind an injectable CommandRunner"
```

---

### Task 4: Wire it together — end-to-end proof against a throwaway IIS site

**Files:**
- Create: `backend/internal/deploy/deploy.go`
- Test: `backend/internal/deploy/deploy_test.go`
- Create: `backend/cmd/deploydemo/main.go` (a small, throwaway CLI for the manual verification step — not part of the server, deleted or kept as a standing debug tool at the project owner's discretion after this task)

**Interfaces:**
- Consumes: `Builder.Build` (Task 1), `VersionStore.NewRelease`/`List`/`Prune` (Task 2), `IISSwapper.SetPhysicalPath` (Task 3).
- Produces: `deploy.Pipeline` with `NewPipeline(builder *Builder, versions *VersionStore, iis *IISSwapper) *Pipeline` and `(*Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int) (releaseDir string, err error)`. No other package consumes this yet — a future plan wires it into an HTTP-triggered, approval-gated request.

- [ ] **Step 1: Write the failing test for the wired-together pipeline (fake IIS)**

Create `backend/internal/deploy/deploy_test.go`:
```go
package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPipeline_Deploy_BuildsVersionsAndSwaps(t *testing.T) {
	requireTool(t, "npm")

	source, err := filepath.Abs("testdata/npm-fixture")
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}

	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner))

	releaseDir, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 5)
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(releaseDir, "index.html")); err != nil {
		t.Errorf("expected built output in release dir: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("got %d appcmd calls, want 1", len(runner.calls))
	}
	lastArg := runner.calls[0][len(runner.calls[0])-1]
	if lastArg != "/physicalPath:"+releaseDir {
		t.Errorf("appcmd physicalPath arg = %q, want %q", lastArg, "/physicalPath:"+releaseDir)
	}
}

func TestPipeline_Deploy_PrunesOldReleases(t *testing.T) {
	requireTool(t, "npm")

	source, _ := filepath.Abs("testdata/npm-fixture")
	vs := NewVersionStore(t.TempDir())
	runner := &fakeCommandRunner{}
	pipeline := NewPipeline(&Builder{}, vs, NewIISSwapper(runner))

	for i := 0; i < 3; i++ {
		if _, err := pipeline.Deploy(source, RecipeNpm, "sample", "test", "DevPlatform Test Site", 2); err != nil {
			t.Fatalf("Deploy #%d returned error: %v", i, err)
		}
	}

	releases, err := vs.List("sample", "test")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases after 3 deploys with keepVersions=2, want 2", len(releases))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/deploy/... -run TestPipeline -v`
Expected: FAIL — `Pipeline`/`NewPipeline` don't exist yet.

- [ ] **Step 3: Implement `deploy.go`**

Create `backend/internal/deploy/deploy.go`:
```go
package deploy

import "fmt"

// Pipeline wires the build, versioning, and IIS-swap steps into one
// deploy operation.
type Pipeline struct {
	builder  *Builder
	versions *VersionStore
	iis      *IISSwapper
}

// NewPipeline returns a Pipeline using the given collaborators.
func NewPipeline(builder *Builder, versions *VersionStore, iis *IISSwapper) *Pipeline {
	return &Pipeline{builder: builder, versions: versions, iis: iis}
}

// Deploy builds sourceDir with recipe, writes the result to a fresh
// versioned release directory for (repo, environment), points siteName at
// that directory, and prunes old releases beyond keepVersions. Returns the
// new release directory's path.
//
// If the build step fails, no release directory is left half-written that
// could later be mistaken for a real release: NewRelease only creates the
// directory, and a failed Build leaves an empty (not partially-populated
// in a way that matters — build tools are expected to fail before writing
// partial output, which both dotnet publish and this plan's npm fixture
// do) directory behind. Cleaning up an empty failed-release directory is
// not implemented in this proof-of-concept task; note it as a follow-up
// if this pipeline is extended to handle build failures more gracefully.
func (p *Pipeline) Deploy(sourceDir string, recipe Recipe, repo, environment, siteName string, keepVersions int) (string, error) {
	releaseDir, err := p.versions.NewRelease(repo, environment)
	if err != nil {
		return "", fmt.Errorf("deploy: failed to allocate release dir: %w", err)
	}

	if err := p.builder.Build(sourceDir, recipe, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: build failed: %w", err)
	}

	if err := p.iis.SetPhysicalPath(siteName, releaseDir); err != nil {
		return "", fmt.Errorf("deploy: failed to activate release: %w", err)
	}

	if err := p.versions.Prune(repo, environment, keepVersions); err != nil {
		return "", fmt.Errorf("deploy: deploy succeeded but pruning old releases failed: %w", err)
	}

	return releaseDir, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/deploy/... -v`
Expected: PASS, all tests across all 4 tasks.

- [ ] **Step 5: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/deploy/deploy.go backend/internal/deploy/deploy_test.go
git commit -m "feat: Pipeline wiring build, versioning, and IIS swap into one deploy operation"
```

- [ ] **Step 7: Build the manual-verification CLI**

This is the one part of this plan that genuinely touches the real (throwaway) IIS site and needs the project owner's elevated PowerShell window — everything above is proven by automated tests; this step is proven by a human watching it happen.

Create `backend/cmd/deploydemo/main.go`:
```go
// Command deploydemo is a throwaway manual-verification tool for the
// internal/deploy package's Pipeline — it is not part of the DevPlatform
// server and is not wired into main.go. It deploys the npm test fixture
// to a real IIS site via the real appcmd.exe, so a human can watch the
// whole build -> version -> swap -> rollback cycle happen for real, once,
// before this mechanism is trusted with anything that matters.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
)

func main() {
	siteName := flag.String("site", "DevPlatform Test Site", "IIS site name to deploy to")
	dataDir := flag.String("data-dir", "./deploydemo-data", "where release folders are stored")
	flag.Parse()

	sourceDir, err := filepath.Abs("internal/deploy/testdata/npm-fixture")
	if err != nil {
		log.Fatal(err)
	}

	vs := deploy.NewVersionStore(*dataDir)
	pipeline := deploy.NewPipeline(&deploy.Builder{}, vs, deploy.NewIISSwapper(deploy.RealCommandRunner{}))

	releaseDir, err := pipeline.Deploy(sourceDir, deploy.RecipeNpm, "demo", "test", *siteName, 5)
	if err != nil {
		log.Fatalf("deploy failed: %v", err)
	}

	fmt.Printf("Deployed to %s\nRelease directory: %s\n", *siteName, releaseDir)
	_ = os.Stdout
}
```

- [ ] **Step 8: Manual verification — the project owner creates a throwaway IIS site**

This must be run by the project owner in an elevated PowerShell window (the same kind used to install the IIS feature earlier), since neither an implementer subagent nor the controller session has Administrator privileges in this environment. Hand these exact commands to the project owner and wait for their confirmation before continuing:

```powershell
# Create a folder for the site to initially point at (content doesn't matter, it'll be replaced by the first deploy)
New-Item -ItemType Directory -Force -Path C:\devplatform-test-site

# Create the IIS site on a port that doesn't collide with the DevPlatform backend (8080) or frontend dev server (5173)
New-WebSite -Name "DevPlatform Test Site" -Port 8090 -PhysicalPath C:\devplatform-test-site -Force

# Confirm it's running
Get-Website -Name "DevPlatform Test Site"
```

- [ ] **Step 9: Manual verification — run the deploy demo**

Also run by the project owner, in the same elevated window, from the `backend` directory:
```powershell
go build -o deploydemo.exe ./cmd/deploydemo
.\deploydemo.exe
```
Expected output: `Deployed to DevPlatform Test Site` followed by a release directory path. Then, in a browser, visit `http://localhost:8090/` — expected: the fixture's page, "deploy fixture build ok".

Run `.\deploydemo.exe` a second time (simulating a second deploy) — expected: succeeds again, a new release directory, the site still serves correctly. Then verify rollback is possible by hand: list the two release directories under `.\deploydemo-data\demo\test\`, and manually run (still elevated):
```powershell
appcmd set vdir "DevPlatform Test Site/" /physicalPath:"<the older release directory's full path>"
```
Refresh the browser — the site should still work (this fixture's two deploys produce identical content, so this step is really about confirming `appcmd` accepts a real prior release path without error, not about seeing a visible difference).

- [ ] **Step 10: Record the verification outcome**

Whoever runs this task (implementer or the project owner reporting back through them) must record in the task report: whether Steps 8-9 were actually run, their real output, and whether the browser check succeeded — not an assumption that it would work. If the project owner isn't available to run the elevated steps during this task's execution, report `DONE_WITH_CONCERNS` clearly stating Steps 8-9 are unverified, rather than claiming completion.

- [ ] **Step 11: Commit the demo CLI**

```bash
git add backend/cmd/deploydemo
git commit -m "feat: deploydemo CLI for manual end-to-end verification of the deploy pipeline"
```

---

## Self-Review Notes

- **Spec coverage:** Covers the design doc's Faz 2 "Deploy akışı" steps 1-2-4 (build → versioned folder → physical-path swap) and the "Versiyon saklama"/"Rollback" bullets (retention via `Prune`, rollback is "point IIS at an older release directory," which `IISSwapper.SetPhysicalPath` already does — no separate rollback code path needed). Explicitly NOT covered, by design: step 3 (secrets copy — no secrets store exists yet), the approval-gated request workflow around this pipeline, and connecting to the real Intranet-F/Intranet-B projects — all named as deferred, not omitted by oversight.
- **Placeholder scan:** No TBD/TODO. The one place this plan is honest about a real limitation (`copyDir`'s flat-only npm output copy) is explicitly flagged as a narrow, deliberate scope boundary for this proof-of-concept, not a placeholder.
- **Type consistency:** `Recipe`/`RecipeDotnet`/`RecipeNpm` (Task 1) are used identically in Task 4's `Pipeline.Deploy`. `VersionStore.NewRelease`'s return value (a directory path) is exactly what `Builder.Build`'s `outputDir` parameter expects and what `IISSwapper.SetPhysicalPath`'s `path` parameter expects — traced through `Pipeline.Deploy` in Task 4. `CommandRunner`/`RealCommandRunner`/`fakeCommandRunner` (Task 3) are reused without modification in Task 4's tests.
- **Security:** every `exec.Command` call in this plan (dotnet, npm, appcmd) uses a fixed program name and an argument list built from typed/validated values, never a shell and never string-concatenated user input — matching the design doc's explicit constraint. The one live-system-touching step (`appcmd` against a real IIS site) is isolated to a throwaway site on a non-production port, run manually by a human with the privileges it actually requires, not automated with elevated service credentials this plan doesn't have and shouldn't casually acquire.
