# Git Smart-HTTP Server + Branch Protection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a real `git` client `clone`/`push` against the DevPlatform server over HTTP, gated by HTTP Basic Auth, with direct pushes to `refs/heads/main` rejected at the server — the core security property the whole project exists to deliver.

**Architecture:** Adopt `go-git/v6`'s ready-made HTTP git server (`github.com/go-git/go-git/v6/backend`, package `backend`, type `Backend` implementing `http.Handler`) instead of hand-rolling the git wire protocol — this was a deliberate, researched decision (see the design doc's "Git Sunucusu — Teknoloji Kararı" section) to avoid reimplementing pkt-line framing ourselves. Repository resolution goes through `transport.NewFilesystemLoader`, wrapped in a small decorator that rejects reference updates to protected branches by intercepting `storage.Storer.SetReference`/`CheckAndSetReference` — not by parsing the wire protocol. Branch protection therefore rests on Go interface embedding, not on hand-verified byte-level protocol code.

**Tech Stack:** Go 1.22+, `github.com/go-git/go-git/v6` (replaces the v5 dependency from the previous plan), `github.com/go-git/go-billy/v5/osfs`, the system `git` CLI (already installed, v2.51.0) as the integration-test oracle.

## Global Constraints

- This plan **migrates the `repostore` package from go-git v5 to go-git v6** in Task 1 — after this plan, `go.mod` should reference `github.com/go-git/go-git/v6`, not `v5`. Do not leave both versions as dependencies once Task 1 is committed.
- **v6 is an alpha release** (currently `v6.0.0-alpha.5` at time of writing). Run `go get github.com/go-git/go-git/v6@latest` exactly once, in Task 1, to resolve and pin whatever the newest alpha is at that moment — `go get` writes the resolved version into `go.mod`/`go.sum`. After Task 1's commit, that pinned version is the dependency: do not re-run `@latest` in any later task or on a future run of this plan, since alpha tags can introduce breaking changes between them and an unreviewed silent upgrade is exactly the risk pinning avoids. Bumping the version later is a deliberate, separate decision, not something that happens implicitly. Known past go-git CVEs (SSH RCE, HTTP credential leak, path validation) are fixed as of v6.0.0-alpha.4 and v5.19.1 — no unpatched vulnerability risk either way, this was checked before choosing v6.
- **This plan's code was researched via `go doc` and the go-git source on GitHub, not verified by compiling it** (the exact package didn't exist as an installed dependency until Task 1 runs `go get`). Treat the code in each task as a well-researched starting point, not verbatim ground truth: if it doesn't compile or a test doesn't pass as written, **read the actual installed package** (`go doc github.com/go-git/go-git/v6/...`, or the source under `$(go env GOMODCACHE)/github.com/go-git/go-git/v6@<version>/`) to find the real signature, adjust, and note the correction in your report. This is expected, not a sign something is wrong with the plan.
- **Correctness bar is the real `git` CLI, not hand-verified wire bytes.** Every task that touches the git-serving path must include at least one integration test that shells out to the actual `git` binary (`os/exec`) against a running `httptest.Server` — clone/push either succeeding or being rejected the way a real git user would experience it. A test that only inspects Go-level types without a real `git` round-trip does not adequately cover this plan's core risk.
- Security: every user-controllable input reaching the filesystem is validated before use (established in the previous plan, still applies — this plan doesn't add new raw filesystem-path handling, it reuses `repostore`/`FilesystemLoader`). HTTP Basic Auth credentials are compared with `crypto/subtle.ConstantTimeCompare`, never `==`, to avoid timing side-channels.
- Commit after every task; each commit must leave `go build ./...` and `go test ./...` passing. Tests that shell out to `git` should skip gracefully (`t.Skip`) if `git` is not found on `PATH` — but do not rely on that skip path yourself; `git` is confirmed installed in this environment (v2.51.0).
- All code comments in English; commit messages Conventional-Commits-ish (`feat:`/`fix:`/`test:`), in English.
- Real AD authentication is **out of scope** for this plan — Task 3 adds a minimal, single-credential HTTP Basic Auth stub (configured via env vars) purely so branch protection (Task 4) has an identity concept to exist alongside. It is explicitly a placeholder for a future AD-integration plan, not a design decision to keep Basic Auth long-term.
- Push-time secret scanning is explicitly **out of scope for this plan** — it is deferred to a separate, future plan (see design doc). Do not add it here even opportunistically.

---

### Task 1: Migrate repostore to go-git v6, default new repos to `main`

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Modify: `backend/internal/repostore/repostore.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: no interface change — `repostore.New`, `Store.Create`, `Store.List` keep their exact existing signatures from the previous plan. Only the import path underneath changes, plus new repos get an explicit `refs/heads/main` default branch instead of git's historical default.

- [ ] **Step 1: Add go-git v6 and remove v5**

Run from `backend/`:
```bash
cd backend
go get github.com/go-git/go-git/v6@latest
go mod edit -droprequire github.com/go-git/go-git/v5
go mod tidy
```
Expected: `go.mod` now requires `github.com/go-git/go-git/v6` and no longer references `github.com/go-git/go-git/v5`. If `go mod tidy` errors about the v5 require still being used somewhere, that means Step 2 hasn't happened yet — do Step 2 first, then re-run `go mod tidy`.

- [ ] **Step 2: Update the import and PlainInit call in repostore.go**

Open `backend/internal/repostore/repostore.go`. Change the import:
```go
// before
"github.com/go-git/go-git/v5"
// after
"github.com/go-git/go-git/v6"
"github.com/go-git/go-git/v6/plumbing"
```

Find the existing call (from the previous plan):
```go
if _, err := git.PlainInit(path, true); err != nil {
```
Replace it with a version that sets the default branch to `main` explicitly, using v6's variadic `InitOption`:
```go
if _, err := git.PlainInit(path, true, git.WithDefaultBranch(plumbing.NewBranchReferenceName("main"))); err != nil {
```
If `git.WithDefaultBranch` or `plumbing.NewBranchReferenceName` don't exist under those exact names in the installed v6 version, run `go doc github.com/go-git/go-git/v6 PlainInit` and `go doc github.com/go-git/go-git/v6/plumbing NewBranchReferenceName` to find the actual names — the previous plan's PlainInit call (without this option) is a safe fallback if no equivalent option exists; note which you used in your report.

Everything else in `repostore.go` (the `os.Mkdir`-based atomic create, the `os.RemoveAll` cleanup on `PlainInit` failure, `validName`, `reservedNames`) stays exactly as-is — this task only touches the import and the `PlainInit` call.

- [ ] **Step 3: Run the existing repostore test suite**

Run: `go test ./internal/repostore/... -v`
Expected: all existing tests (from the previous plan) still PASS. None of them assert on the default branch name, so this step just confirms the migration didn't break anything.

- [ ] **Step 4: Add a test asserting the new default branch**

Add to `backend/internal/repostore/repostore_test.go`:
```go
func TestCreate_SetsMainAsDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	path, err := store.Create("branch-check")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	headBytes, err := os.ReadFile(filepath.Join(path, "HEAD"))
	if err != nil {
		t.Fatalf("failed to read HEAD: %v", err)
	}

	head := strings.TrimSpace(string(headBytes))
	if head != "ref: refs/heads/main" {
		t.Errorf("HEAD = %q, want %q", head, "ref: refs/heads/main")
	}
}
```
This reads the bare repo's `HEAD` file directly (no go-git API needed) to confirm the default branch is `main`, not `master`. Add `"strings"` to the imports if not already present.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/repostore/... -v`
Expected: PASS, including the new test. If it fails because `HEAD` still points at `refs/heads/master`, the `WithDefaultBranch` option in Step 2 either wasn't applied correctly or doesn't exist under that name — investigate via `go doc` before giving up on it; if truly unavailable in this v6 alpha version, report DONE_WITH_CONCERNS noting new repos default to `master` instead of `main` (not a blocker, just a note for the next plan or a future fix).

- [ ] **Step 6: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/repostore/repostore.go backend/internal/repostore/repostore_test.go
git commit -m "feat: migrate repostore to go-git v6, default new repos to main"
```

---

### Task 2: Git smart-HTTP server wired into the router

**Files:**
- Create: `backend/internal/gitserver/gitserver.go`
- Test: `backend/internal/gitserver/gitserver_test.go`
- Modify: `backend/internal/server/server.go`
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: `repostore.New`/`Create` (Task 1, previous plan) only in the test, to set up a repo to clone against. Does not consume `repostore.Store` directly in the production code — see note below.
- Produces: `gitserver.NewHandler(dataDir string) http.Handler` — an `http.Handler` that serves the git smart-HTTP protocol for any bare repo under `dataDir`. `server.NewRouter` changes signature to `NewRouter(gitHandler http.Handler) *http.ServeMux` — Task 3 and Task 4 will pass in a wrapped version of the same handler; Task 4's follow-on plans (task board, etc.) can add further `NewRouter` parameters later using the same pattern.

**Note on `dataDir` vs `repostore.Store`:** this task points `gitserver`'s repository loader directly at the same `dataDir` that `repostore.Store` manages, via `go-git`'s own `transport.NewFilesystemLoader`, rather than routing through `repostore.Store` itself. `repostore.Store` remains the *creation/listing* API (used by whatever later task exposes "create a new repo" over HTTP); `gitserver` is purely the *clone/push* protocol layer over the same directory. This mirrors how real git hosting works: repo creation and repo serving are different concerns over the same storage.

- [ ] **Step 1: Write the failing integration test**

Create `backend/internal/gitserver/gitserver_test.go`:
```go
package gitserver

import (
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not found on PATH, skipping integration test")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput:\n%s", args, err, out)
	}
	return string(out)
}

func TestClone_AfterInitialPush(t *testing.T) {
	requireGit(t)

	// Deliberately NOT testing a clone of a genuinely empty (zero-ref)
	// repo here: go-git v6's server-side advertisement path for that case
	// wasn't independently confirmed during planning (see Global
	// Constraints), so this test pushes one commit first to stay on
	// well-understood ground — "can a client clone a repo that has
	// content" — and isolates the zero-ref edge case out of this task's
	// pass/fail bar. If you want to explore the zero-ref case, do it as
	// an experiment outside this test, not as a change to its assertion.
	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("hello"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	seed := t.TempDir()
	runGit(t, seed, "init", "-b", "main")
	runGit(t, seed, "config", "user.email", "test@example.com")
	runGit(t, seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, seed, "add", "README.md")
	runGit(t, seed, "commit", "-m", "seed commit")
	runGit(t, seed, "remote", "add", "origin", srv.URL+"/hello.git")
	runGit(t, seed, "push", "origin", "main")

	cloneDir := t.TempDir()
	cloneTarget := filepath.Join(cloneDir, "hello")
	runGit(t, cloneDir, "clone", srv.URL+"/hello.git", cloneTarget)

	if _, err := os.Stat(filepath.Join(cloneTarget, "README.md")); err != nil {
		t.Errorf("expected cloned README.md to exist: %v", err)
	}
}

func TestPushAndClone_RoundTrip(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("roundtrip"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	// Prepare a local repo with one commit, on a non-default branch name
	// (this task doesn't test branch protection yet — that's Task 4 — but
	// pushing to "main" here would be testing behavior this task doesn't
	// implement any opinion about yet, so use a feature branch name to
	// keep this test's scope to "does push/clone work at all").
	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-x")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/roundtrip.git")
	runGit(t, work, "push", "origin", "feature-x")

	// Clone into a second directory and confirm the pushed content arrived.
	cloneDir := t.TempDir()
	cloneTarget := filepath.Join(cloneDir, "roundtrip")
	runGit(t, cloneDir, "clone", "--branch", "feature-x", srv.URL+"/roundtrip.git", cloneTarget)

	content, err := os.ReadFile(filepath.Join(cloneTarget, "README.md"))
	if err != nil {
		t.Fatalf("failed to read cloned file: %v", err)
	}
	if string(content) != "hello\n" {
		t.Errorf("cloned README.md = %q, want %q", content, "hello\n")
	}
}
```

Add `"os"` to the imports (alongside `"net/http/httptest"`, `"os/exec"`, `"path/filepath"`, `"testing"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gitserver/... -v`
Expected: FAIL — package `gitserver` / `NewHandler` doesn't exist yet.

- [ ] **Step 3: Implement the handler**

Create `backend/internal/gitserver/gitserver.go`:
```go
// Package gitserver exposes DevPlatform's bare git repositories over the
// git smart-HTTP protocol, using go-git's own server implementation
// (github.com/go-git/go-git/v6/backend) rather than hand-rolling the wire
// protocol. See docs/superpowers/specs/2026-08-07-dev-platform-design.md,
// "Git Sunucusu — Teknoloji Kararı", for why.
package gitserver

import (
	"net/http"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// Prefix is the URL path prefix under which the git smart-HTTP endpoints
// are served. A request for repo "foo" is reached at Prefix+"/foo.git/...".
const Prefix = "/git"

// NewHandler returns an http.Handler serving every bare repository under
// dataDir via the git smart-HTTP protocol. Repository names are resolved
// the same way repostore.Store names them (e.g. "foo" on disk as
// "foo.git"); callers must request "/foo.git/...", not "/foo/...".
func NewHandler(dataDir string) http.Handler {
	loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
	b := backend.New(loader)
	b.Prefix = Prefix
	return b
}
```

If `backend.New` doesn't exist under that exact name (the researched source used both `New` and `NewBackend` in different snippets — the installed version may differ), check with `go doc github.com/go-git/go-git/v6/backend` and use whichever constructor is actually exported; if neither exists, constructing `&backend.Backend{Loader: loader, Prefix: Prefix}` directly (per the `Backend` struct's exported fields) is an equally valid fallback — `Backend`'s fields were confirmed exported in research.

- [ ] **Step 4: Wire the handler into the router**

Modify `backend/internal/server/server.go` — change `NewRouter` to accept the git handler and mount it:
```go
package server

import (
	"encoding/json"
	"net/http"
)

// NewRouter builds the top-level HTTP router. gitHandler serves the git
// smart-HTTP protocol under its own prefix (see internal/gitserver).
func NewRouter(gitHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.Handle("/git/", gitHandler)
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```
Note the pattern `"/git/"` here is deliberately the same literal prefix as `gitserver.Prefix` (`"/git"`) — `http.ServeMux`'s `/git/` pattern matches any path starting with `/git/`, and `gitserver.NewHandler`'s internal `Backend.Prefix = "/git"` then strips that same prefix before its own route matching runs. If you change `gitserver.Prefix`, change the mux pattern here to match, and vice versa — they must stay in sync. Consider adding a one-line comment at each site pointing at the other, since nothing in the type system enforces this.

Update `backend/internal/server/server_test.go` — its existing `TestHealthz_ReturnsOK` test calls `NewRouter()` with no arguments; update the call site to `NewRouter(http.NotFoundHandler())` (a no-op placeholder handler is sufficient for a test that only exercises `/healthz`, which never reaches the git handler).

- [ ] **Step 5: Wire main.go**

Modify `backend/cmd/devplatform/main.go` to construct and pass the git handler:
```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/kenissha/DevPlatform/backend/internal/config"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/server"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Fatalf("failed to create data dir %s: %v", cfg.DataDir, err)
	}

	store := repostore.New(cfg.DataDir)
	repos, err := store.List()
	if err != nil {
		log.Fatalf("failed to list repositories: %v", err)
	}
	log.Printf("repository store ready at %s (%d repos)", cfg.DataDir, len(repos))

	gitHandler := gitserver.NewHandler(cfg.DataDir)
	router := server.NewRouter(gitHandler)

	log.Printf("devplatform listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Run the gitserver integration tests**

Run: `go test ./internal/gitserver/... -v`
Expected: PASS (`TestClone_AfterInitialPush`, `TestPushAndClone_RoundTrip`). If a test fails on a specific `git` subcommand, read the failure's captured `git` output (the `runGit` helper includes it in `t.Fatalf`) — it will usually say plainly what protocol expectation wasn't met, which is far more actionable than reasoning about pkt-lines abstractly.

- [ ] **Step 7: Run the full suite and build**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed, including the updated `server` package test.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/gitserver backend/internal/server/server.go backend/internal/server/server_test.go backend/cmd/devplatform/main.go
git commit -m "feat: serve git smart-HTTP protocol over repostore's data directory"
```

---

### Task 3: Minimal HTTP Basic Auth in front of the git endpoints

**Files:**
- Create: `backend/internal/gitauth/gitauth.go`
- Test: `backend/internal/gitauth/gitauth_test.go`
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: nothing from earlier tasks directly (standalone middleware package), but is applied around the `gitHandler` from Task 2 in `main.go`.
- Produces: `gitauth.RequireBasicAuth(username, password string, next http.Handler) http.Handler` — Task 4's branch-protection work does not need this directly, but a later plan (real AD auth) will replace this function's *implementation*, not its call site in `main.go`, so keep the signature exactly this shape.
- `config.Config` gains two new fields: `GitUsername string`, `GitPassword string`.

- [ ] **Step 1: Write the failing config tests**

Add to `backend/internal/config/config_test.go` (extending the existing `TestLoad_ReadsFromEnv` test, or add new ones — following the existing file's style):
```go
func TestLoad_ReadsGitCredentialsFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_GIT_USERNAME", "devuser")
	os.Setenv("DEVPLATFORM_GIT_PASSWORD", "devpass")
	defer os.Unsetenv("DEVPLATFORM_GIT_USERNAME")
	defer os.Unsetenv("DEVPLATFORM_GIT_PASSWORD")

	cfg := Load()

	if cfg.GitUsername != "devuser" {
		t.Errorf("GitUsername = %q, want %q", cfg.GitUsername, "devuser")
	}
	if cfg.GitPassword != "devpass" {
		t.Errorf("GitPassword = %q, want %q", cfg.GitPassword, "devpass")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL — `cfg.GitUsername`/`cfg.GitPassword` don't exist yet (compile error).

- [ ] **Step 3: Add the fields to Config**

Modify `backend/internal/config/config.go`:
```go
package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr  string
	DataDir     string
	GitUsername string
	GitPassword string
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
func Load() Config {
	return Config{
		ListenAddr:  getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:     getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		GitUsername: getEnv("DEVPLATFORM_GIT_USERNAME", "dev"),
		GitPassword: getEnv("DEVPLATFORM_GIT_PASSWORD", "dev"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```
The `"dev"/"dev"` defaults exist only so the server boots without configuration during local development — note this plainly in your commit, it is not meant as a real credential and every environment beyond a developer's own machine must set both env vars explicitly.

- [ ] **Step 4: Run config tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS, including the pre-existing tests from the previous plan.

- [ ] **Step 5: Write the failing gitauth tests**

Create `backend/internal/gitauth/gitauth_test.go`:
```go
package gitauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireBasicAuth_RejectsMissingCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestRequireBasicAuth_RejectsWrongCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	req.SetBasicAuth("user", "wrong-password")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireBasicAuth_AllowsCorrectCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	req.SetBasicAuth("user", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/gitauth/... -v`
Expected: FAIL — package `gitauth` doesn't exist yet.

- [ ] **Step 7: Implement the middleware**

Create `backend/internal/gitauth/gitauth.go`:
```go
// Package gitauth provides a minimal HTTP Basic Auth gate for the git
// smart-HTTP endpoints. This is a deliberate placeholder: a future plan
// replaces the credential check with real Active Directory authentication
// without changing this package's call site in main.go.
package gitauth

import (
	"crypto/subtle"
	"net/http"
)

// RequireBasicAuth wraps next with an HTTP Basic Auth check against a
// single configured username/password. Requests without valid credentials
// receive 401 with a WWW-Authenticate challenge, matching what a `git`
// client expects in order to prompt for credentials.
func RequireBasicAuth(username, password string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !constantTimeEqual(user, username) || !constantTimeEqual(pass, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform Git"`)
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
```

- [ ] **Step 8: Run gitauth tests to verify they pass**

Run: `go test ./internal/gitauth/... -v`
Expected: PASS (all 3 tests).

- [ ] **Step 9: Wire the middleware into main.go**

Modify `backend/cmd/devplatform/main.go` — wrap `gitHandler` before passing it to `server.NewRouter`:
```go
	gitHandler := gitserver.NewHandler(cfg.DataDir)
	authedGitHandler := gitauth.RequireBasicAuth(cfg.GitUsername, cfg.GitPassword, gitHandler)
	router := server.NewRouter(authedGitHandler)
```
Add `"github.com/kenissha/DevPlatform/backend/internal/gitauth"` to the import block — copy the module path prefix (`github.com/kenissha/DevPlatform/backend/...`) from one of the other internal imports already in this file rather than retyping it, to avoid a typo.

- [ ] **Step 10: Update the gitserver integration tests to pass credentials**

The `git clone`/`git push` calls in `backend/internal/gitserver/gitserver_test.go` (Task 2) did not go through auth because that test constructs `NewHandler` directly, without wrapping it in `gitauth.RequireBasicAuth` — so those tests are unaffected by this task and should still pass as-is. Run them to confirm: `go test ./internal/gitserver/... -v` — expected PASS, unchanged.

If you'd like an end-to-end test proving auth and git-serving compose correctly together, that's what Task 4's integration tests will exercise (they build the full stack including auth) — do not duplicate that here.

- [ ] **Step 11: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed.

- [ ] **Step 12: Commit**

```bash
git add backend/internal/gitauth backend/internal/config/config.go backend/internal/config/config_test.go backend/cmd/devplatform/main.go
git commit -m "feat: gate git smart-HTTP endpoints behind HTTP Basic Auth"
```

---

### Task 4: Branch protection — reject direct pushes to `refs/heads/main`

**Files:**
- Create: `backend/internal/gitserver/protectedloader.go`
- Test: `backend/internal/gitserver/protectedloader_test.go`
- Modify: `backend/internal/gitserver/gitserver.go`

**Interfaces:**
- Consumes: `transport.Loader` (the interface `transport.NewFilesystemLoader` already returns, from Task 2).
- Produces: `gitserver.NewHandler` behavior changes (not its signature) — pushes to `refs/heads/main` are now rejected server-side for every repository served by this handler. No other task depends on new exported symbols from this task; the protection is an internal wrapping detail.

- [ ] **Step 1: Write the failing integration test**

Create `backend/internal/gitserver/protectedloader_test.go`:
```go
package gitserver

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func TestPush_DirectlyToMain_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("protected"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected.git")

	cmd := exec.Command("git", "push", "origin", "main")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `git push origin main` to fail, but it succeeded. Output:\n%s", out)
	}
	t.Logf("git push to main correctly failed with:\n%s", out)

	// The real assertion: main must NOT exist on the server afterward,
	// regardless of exactly how git's CLI worded the rejection.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "protected")
	cmd = exec.Command("git", "clone", srv.URL+"/protected.git", cloneTarget)
	cmd.Dir = verifyDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to clone for verification: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/main exists on the server, but the push to it should have been rejected")
	}
}

func TestPush_DeleteMain_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	bareRepoPath, err := store.Create("protected3")
	if err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")

	// Seed "main" onto the server by pushing straight to the bare repo's
	// path on disk (git's local file transport), bypassing the HTTP server
	// and its protection entirely. This is test setup, not the behavior
	// under test: this plan has no HTTP-reachable way to get "main" onto
	// the server (that's the point of TestPush_DirectlyToMain_IsRejected),
	// so seeding it locally is the only way to construct a repo state
	// where "delete main" has an existing ref to actually try deleting.
	runGit(t, work, "push", bareRepoPath, "main")

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected3.git")

	cmd := exec.Command("git", "push", "origin", "--delete", "main")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `git push origin --delete main` to fail, but it succeeded. Output:\n%s", out)
	}
	t.Logf("git push --delete main correctly failed with:\n%s", out)

	// Confirm main is still there after the rejected delete attempt.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "protected3")
	runGit(t, verifyDir, "clone", srv.URL+"/protected3.git", cloneTarget)
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/main")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err != nil {
		t.Fatal("refs/heads/main no longer exists on the server after the rejected delete attempt")
	}
}

func TestPush_ToFeatureBranch_StillSucceeds(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("protected2"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-y")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "initial commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/protected2.git")
	runGit(t, work, "push", "origin", "feature-y")
	// runGit already fails the test via t.Fatalf if this push is rejected.
}
```
This file's imports should be `"net/http/httptest"`, `"os"`, `"os/exec"`, `"path/filepath"`, `"testing"`, plus the `repostore` import shown above. `os/exec` is used directly in `TestPush_DirectlyToMain_IsRejected` for the push that's expected to fail (the existing `runGit` helper from Task 2's test file always calls `t.Fatalf` on failure, and this specific test needs to inspect the failure instead of treating it as fatal) and again for the verification clone/rev-parse calls; `os` is used for `os.WriteFile`.

- [ ] **Step 2: Run to verify the first test fails (protection not yet implemented)**

Run: `go test ./internal/gitserver/... -run TestPush_DirectlyToMain_IsRejected -v`
Expected: FAIL — the push currently succeeds (no protection yet), so the test's `t.Fatalf("expected ... to fail, but it succeeded")` triggers.

- [ ] **Step 3: Implement the protecting loader**

Create `backend/internal/gitserver/protectedloader.go`:
```go
package gitserver

import (
	"fmt"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
)

// protectedRefs lists reference names that can never be updated by a
// direct push through this server. Pushing to one of these is expected to
// go through a review/merge flow in a later plan instead.
var protectedRefs = map[plumbing.ReferenceName]bool{
	plumbing.NewBranchReferenceName("main"): true,
}

// protectingLoader wraps a transport.Loader so every storer it returns
// rejects reference updates to protectedRefs.
type protectingLoader struct {
	inner transport.Loader
}

func newProtectingLoader(inner transport.Loader) transport.Loader {
	return &protectingLoader{inner: inner}
}

func (l *protectingLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &protectedStorer{Storer: st}, nil
}

// protectedStorer embeds a real storage.Storer so every method is
// delegated automatically via Go's interface embedding, except the
// reference-write methods, which are overridden to reject protected refs.
type protectedStorer struct {
	storage.Storer
}

func (s *protectedStorer) SetReference(ref *plumbing.Reference) error {
	if protectedRefs[ref.Name()] {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", ref.Name())
	}
	return s.Storer.SetReference(ref)
}

func (s *protectedStorer) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	if protectedRefs[newRef.Name()] {
		return fmt.Errorf("gitserver: direct push to protected ref %q is not allowed", newRef.Name())
	}
	return s.Storer.CheckAndSetReference(newRef, old)
}
```

This plan's Global Constraints note that the exact go-git v6 API wasn't verified by compiling — but the ref-update call path specifically **was** independently confirmed by reading `plumbing/transport/receive_pack.go`'s `updateReferences` function on GitHub during planning: it calls `st.SetReference(ref)` for both ref creation and ref update, and — importantly — calls `st.RemoveReference(cmd.Name)` for ref **deletion**. The code above only overrides `SetReference`/`CheckAndSetReference`, which stops `git push origin main` from changing where `main` points, but does **not** stop `git push origin --delete main` from deleting the branch entirely — a real gap, not a hypothetical one. Add a third override to close it:
```go
func (s *protectedStorer) RemoveReference(name plumbing.ReferenceName) error {
	if protectedRefs[name] {
		return fmt.Errorf("gitserver: deleting protected ref %q is not allowed", name)
	}
	return s.Storer.RemoveReference(name)
}
```

If `storage.Storer`'s package path or `plumbing.Reference`/`ReferenceName`/`NewBranchReferenceName` differ from what's written here once you check the installed v6 version (`go doc github.com/go-git/go-git/v6/storage Storer`, `go doc github.com/go-git/go-git/v6/plumbing`), adjust the import paths and type names accordingly — the pattern (embed the interface, override the write methods) is what matters, not the exact import spelling guessed here.

- [ ] **Step 4: Wire the protecting loader into NewHandler**

Modify `backend/internal/gitserver/gitserver.go` — wrap the loader before constructing the backend:
```go
func NewHandler(dataDir string) http.Handler {
	loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
	protected := newProtectingLoader(loader)
	b := backend.New(protected)
	b.Prefix = Prefix
	return b
}
```

- [ ] **Step 5: Run the protection tests**

Run: `go test ./internal/gitserver/... -v`
Expected: PASS — all three new tests (`TestPush_DirectlyToMain_IsRejected`, `TestPush_DeleteMain_IsRejected`, `TestPush_ToFeatureBranch_StillSucceeds`) and all of Task 2's tests (`TestClone_AfterInitialPush`, `TestPushAndClone_RoundTrip` — neither of those pushes to or deletes `main`, so they remain unaffected).

If `TestPush_DirectlyToMain_IsRejected` still fails at the "server must not have main" verification step (not just the push-command-fails step), that's a real finding, not a test bug: it would mean the server accepted the pack data but failed the ref update in a way that still left `main` partially updated, or reported failure while actually applying the change. Do not weaken the test to make it pass — investigate `transport.ReceivePack`'s actual behavior (`go doc github.com/go-git/go-git/v6/plumbing/transport ReceivePack` or read its source) and report exactly what you find if the assertion can't be made to hold; this is the single most important behavior in this entire plan and is worth an accurate BLOCKED report over a silently-loosened test.

- [ ] **Step 6: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed, full suite green (config, repostore, server, gitauth, gitserver).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/gitserver/protectedloader.go backend/internal/gitserver/protectedloader_test.go backend/internal/gitserver/gitserver.go
git commit -m "feat: reject direct pushes to refs/heads/main at the storer level"
```

- [ ] **Step 8: Push**

```bash
git push origin main
```

---

## Self-Review Notes

- **Spec coverage:** Covers the design doc's Faz 1 git-hosting items: self-hosted smart-HTTP git serving, and branch protection enforced "at the protocol level" (here: at the storage-interface level, which is stronger than a protocol-parsing check since it can't be bypassed by any client that speaks the wire protocol correctly — every write path funnels through `SetReference`/`CheckAndSetReference`). AD auth, task board, merge-request diff review, audit log, and push-time secret scanning are explicitly out of scope per Global Constraints and the design doc's phasing — not silently dropped.
- **Placeholder scan:** No TBD/TODO. Task 1 and Task 4 both contain explicit "if the API differs, do X" contingency instructions rather than a placeholder — this is intentional given the Global Constraints' note that this plan's go-git v6 code is researched, not compiled, and is flagged as such rather than presented as certain.
- **Type consistency:** `gitserver.NewHandler(dataDir string) http.Handler` (Task 2) keeps the same signature through Task 4 — Task 4 changes its internals (wrapping the loader) without changing the signature, so `main.go`'s call site from Task 2 needs no changes in Task 4. `server.NewRouter(gitHandler http.Handler) *http.ServeMux` (Task 2) is consumed identically in Task 3 (just with a wrapped handler passed in) and unchanged in Task 4. `config.Config`'s new `GitUsername`/`GitPassword` fields (Task 3) are read only in `main.go`, no other task depends on them.
- **Security:** the core property this plan delivers — direct pushes to `main` are impossible even for an authenticated user, whether the push updates, creates, or deletes the ref — is enforced at the Go interface level (`SetReference`, `CheckAndSetReference`, and `RemoveReference` all funnel through the same guard), not by pattern-matching HTTP requests or trusting client-declared intent. The `RemoveReference` override was added after independently reading `plumbing/transport/receive_pack.go`'s `updateReferences` function on GitHub during planning and confirming it calls `RemoveReference` for branch deletion — a real gap the plan would otherwise have shipped with (protecting against unwanted updates while leaving deletion open). Basic Auth uses constant-time comparison. The plan explicitly does not claim push-time secret scanning or real AD auth; both are named and deferred, not omitted by oversight.
