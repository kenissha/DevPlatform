# Backend Foundation + Repository Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the DevPlatform Go backend skeleton — a buildable, testable HTTP service with configuration loading and a bare-git-repository storage layer — as the foundation the git hosting server (next plan) will be built on.

**Architecture:** A single Go module (`backend/`) with a small `cmd/devplatform` entrypoint and focused `internal/` packages (`config`, `server`, `repostore`). No HTTP endpoints for repository management yet — `repostore` is a standalone, fully-tested package that later plans (git smart-HTTP server, task API) will wire into the HTTP router. This plan produces no UI.

**Tech Stack:** Go 1.22+, standard library `net/http` (method-pattern routing), `github.com/go-git/go-git/v5` for repository initialization.

## Global Constraints

- Module path: `github.com/kenissha/DevPlatform/backend` (matches the GitHub repo casing exactly).
- No external runtime dependency: the backend must build to a single static binary (`go build`), no CGO, no external git binary invocation.
- Security: every user-controllable input that reaches the filesystem (e.g. repository names) must be validated against an explicit allow-list pattern before use — never interpolated into a path or command unchecked. This is a standing project requirement (see design doc's security notes), not specific to this plan.
- Commit after every task (see steps below); each commit must leave `go build ./...` and `go test ./...` passing.
- All code comments in English (matches the README/repo convention already pushed); commit messages follow the existing repo's style (short, imperative, Conventional-Commits-ish prefix like `feat:`/`test:`/`chore:`).

---

### Task 1: Project skeleton + health check endpoint

**Files:**
- Create: `backend/go.mod`
- Create: `backend/cmd/devplatform/main.go`
- Create: `backend/internal/server/server.go`
- Test: `backend/internal/server/server_test.go`

**Interfaces:**
- Produces: `server.NewRouter() *http.ServeMux` — later tasks/plans (git HTTP handlers, task API) register additional routes on the same pattern by extending this function.

- [ ] **Step 1: Initialize the Go module**

Run from `backend/`:
```bash
cd backend
go mod init github.com/kenissha/DevPlatform/backend
```
Expected: creates `backend/go.mod` with `module github.com/kenissha/DevPlatform/backend` and a `go 1.22` (or newer installed) directive.

- [ ] **Step 2: Write the failing test for the health endpoint**

Create `backend/internal/server/server_test.go`:
```go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if body := rec.Body.String(); body != `{"status":"ok"}`+"\n" {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`+"\n")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/server/... -v`
Expected: FAIL — `NewRouter` undefined (package `server` has no such function yet).

- [ ] **Step 4: Implement the router and health handler**

Create `backend/internal/server/server.go`:
```go
package server

import (
	"encoding/json"
	"net/http"
)

// NewRouter builds the top-level HTTP router. Later packages extend this
// with additional routes (git smart-HTTP, task API, ...).
func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/server/... -v`
Expected: PASS

- [ ] **Step 6: Wire up the entrypoint**

Create `backend/cmd/devplatform/main.go`:
```go
package main

import (
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/server"
)

func main() {
	router := server.NewRouter()

	addr := ":8080"
	log.Printf("devplatform listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 7: Verify it builds and runs**

Run:
```bash
go build ./...
go vet ./...
```
Expected: both succeed with no output/errors.

- [ ] **Step 8: Commit**

```bash
git add backend/go.mod backend/go.sum backend/cmd backend/internal/server
git commit -m "feat: backend skeleton with health check endpoint"
```

---

### Task 2: Configuration loading

**Files:**
- Create: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `config.Config{ListenAddr string, DataDir string}` and `config.Load() Config` — Task 4 (and later plans) read `cfg.DataDir` to know where repositories/data live on disk.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/config/config_test.go`:
```go
package config

import (
	"os"
	"testing"
)

func TestLoad_UsesDefaultsWhenEnvNotSet(t *testing.T) {
	os.Unsetenv("DEVPLATFORM_LISTEN_ADDR")
	os.Unsetenv("DEVPLATFORM_DATA_DIR")

	cfg := Load()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "./data")
	}
}

func TestLoad_ReadsFromEnv(t *testing.T) {
	os.Setenv("DEVPLATFORM_LISTEN_ADDR", ":9090")
	os.Setenv("DEVPLATFORM_DATA_DIR", "/tmp/devplatform")
	defer os.Unsetenv("DEVPLATFORM_LISTEN_ADDR")
	defer os.Unsetenv("DEVPLATFORM_DATA_DIR")

	cfg := Load()

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.DataDir != "/tmp/devplatform" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/tmp/devplatform")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/... -v`
Expected: FAIL — package `config` / `Load` undefined.

- [ ] **Step 3: Implement config loading**

Create `backend/internal/config/config.go`:
```go
package config

import "os"

// Config holds runtime configuration read from environment variables.
type Config struct {
	ListenAddr string
	DataDir    string
}

// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
func Load() Config {
	return Config{
		ListenAddr: getEnv("DEVPLATFORM_LISTEN_ADDR", ":8080"),
		DataDir:    getEnv("DEVPLATFORM_DATA_DIR", "./data"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 5: Wire config into main.go**

Modify `backend/cmd/devplatform/main.go` to match:
```go
package main

import (
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/config"
	"github.com/kenissha/DevPlatform/backend/internal/server"
)

func main() {
	cfg := config.Load()
	router := server.NewRouter()

	log.Printf("devplatform listening on %s (data dir: %s)", cfg.ListenAddr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Verify it builds**

Run: `go build ./...`
Expected: succeeds with no errors.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/config backend/cmd/devplatform/main.go
git commit -m "feat: environment-based configuration loading"
```

---

### Task 3: Repository storage layer

**Files:**
- Create: `backend/internal/repostore/repostore.go`
- Test: `backend/internal/repostore/repostore_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1–2 (standalone package, takes a root directory path).
- Produces: `repostore.Store` with `New(rootDir string) *Store`, `(*Store) Create(name string) (string, error)`, `(*Store) List() ([]string, error)`, and sentinel errors `repostore.ErrInvalidName`, `repostore.ErrAlreadyExists` — the future git smart-HTTP server plan resolves incoming repo names through this store before touching disk, so a malicious repo name (e.g. containing `..` or `/`) is rejected here, not downstream.

- [ ] **Step 1: Add the go-git dependency**

Run:
```bash
cd backend
go get github.com/go-git/go-git/v5
```
Expected: `go.mod`/`go.sum` updated with `github.com/go-git/go-git/v5` and its transitive dependencies.

- [ ] **Step 2: Write the failing tests**

Create `backend/internal/repostore/repostore_test.go`:
```go
package repostore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate_MakesABareRepo(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	path, err := store.Create("intranet-backend")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	want := filepath.Join(dir, "intranet-backend.git")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		t.Errorf("expected bare repo HEAD file to exist: %v", err)
	}
}

func TestCreate_RejectsInvalidName(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("../escape")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsNameWithSlash(t *testing.T) {
	store := New(t.TempDir())

	_, err := store.Create("sub/dir")
	if err != ErrInvalidName {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
}

func TestCreate_RejectsDuplicateName(t *testing.T) {
	store := New(t.TempDir())

	if _, err := store.Create("intranet-backend"); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	_, err := store.Create("intranet-backend")
	if err != ErrAlreadyExists {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestList_ReturnsCreatedRepoNames(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.Create("intranet-backend")
	store.Create("intranet-frontend")

	names, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %v", len(names), names)
	}
}

func TestList_ReturnsEmptySliceWhenDirMissing(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "does-not-exist"))

	names, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("got %d names, want 0", len(names))
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/repostore/... -v`
Expected: FAIL — package `repostore` does not exist yet.

- [ ] **Step 4: Implement the repository store**

Create `backend/internal/repostore/repostore.go`:
```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/repostore/... -v`
Expected: PASS (all 6 tests)

- [ ] **Step 6: Run the full test suite and build**

Run:
```bash
go build ./...
go test ./...
```
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/repostore
git commit -m "feat: bare git repository storage with name validation"
```

---

### Task 4: Wire data directory into startup + manual smoke test

**Files:**
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: `config.Load()` (Task 2), `repostore.New` (Task 3).
- Produces: a running binary that creates its data directory on startup — later plans (git HTTP server) rely on `cfg.DataDir` already existing when they start accepting requests.

- [ ] **Step 1: Update main.go to create the data directory and initialize the store**

Modify `backend/cmd/devplatform/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/kenissha/DevPlatform/backend/internal/config"
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

	router := server.NewRouter()

	log.Printf("devplatform listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
```

Note: `0o750` restricts the data directory to the owner and group (no world access) — the data directory will hold repository contents and, in later plans, other project data, so it should not be world-readable.

- [ ] **Step 2: Build and run a manual smoke test**

Run:
```bash
go build -o devplatform.exe ./cmd/devplatform
DEVPLATFORM_DATA_DIR=./tmp-data ./devplatform.exe &
```
(On Windows PowerShell, set the env var with `$env:DEVPLATFORM_DATA_DIR = "./tmp-data"` first, then run `.\devplatform.exe` in the background or a separate terminal.)

Then in another terminal:
```bash
curl -i http://localhost:8080/healthz
```
Expected: `HTTP/1.1 200 OK` with body `{"status":"ok"}` and a `./tmp-data` directory created on disk.

Stop the server (Ctrl+C or kill the background process) and delete `./tmp-data` and `devplatform.exe` afterward — these are manual test artifacts, not committed.

- [ ] **Step 3: Run the full test suite one last time**

Run: `go test ./... -v`
Expected: all tests across `config`, `server`, `repostore` PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/devplatform/main.go
git commit -m "feat: initialize data directory and repository store on startup"
```

- [ ] **Step 5: Push**

```bash
git push origin main
```

---

## Self-Review Notes

- **Spec coverage:** This plan covers only the "backend foundation" slice implied by the design doc's Faz 1 (a working Go service backend/ can be built on). Git smart-HTTP serving, branch protection, AD auth, task board, and frontend are deliberately **not** in this plan — they are large enough to warrant their own plans, per the design's phased structure. Next plan should be the git smart-HTTP server + protected-branch enforcement, built directly on `repostore` from Task 3.
- **Placeholder scan:** No TBD/TODO; every step has real, complete code.
- **Type consistency:** `config.Config.DataDir` (Task 2) is the same field read by `repostore.New` via `cfg.DataDir` in Task 4 — matches. `repostore.Store.Create`/`List` signatures in Task 3 match their usage in Task 4.
- **Security:** repository names are validated with a strict allow-list regex before any filesystem operation (blocks path traversal via `..` or `/`); the data directory is created with restrictive permissions (`0o750`). No shell commands are constructed anywhere in this plan, so there is no command-injection surface yet — that becomes relevant starting with the build/deploy automation plan (Faz 2) and will get its own explicit treatment there.
