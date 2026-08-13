# IIS Helper Privilege Separation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the one privileged operation in this codebase (running `appcmd.exe` to swap an IIS site's physical path) out of `devplatform.exe` into a separate, narrowly-scoped Windows Service, so the main server process never needs Administrator rights.

**Architecture:** `deploy.IISSwapper` already calls appcmd through a `CommandRunner` interface. A new `internal/iishelper` package defines a tiny request/response protocol and a validator that only accepts the exact "set this known IIS site's physical path to this absolute directory" shape. A new `cmd/iishelper` binary runs as a Windows Service (Administrator-equivalent, via `LocalSystem`), listens on a local named pipe, validates every request against that fixed shape before executing it for real. `devplatform.exe` gets a new `HelperCommandRunner` (same `CommandRunner` interface, zero changes to `IISSwapper` itself) that forwards to the pipe instead of running appcmd directly.

**Tech Stack:** Go 1.26, `github.com/Microsoft/go-winio` (named pipes — already an indirect dependency via go-git), `golang.org/x/sys/windows/svc` (Windows Service lifecycle — already an indirect dependency).

**Spec:** `docs/superpowers/specs/2026-08-13-iis-helper-privilege-separation-design.md`

## Global Constraints

- No free-text ever reaches a subprocess command: the helper only ever executes `appcmd.exe` with exactly the four fixed arguments `ValidateRequest` checks — never anything from an unvalidated source.
- The helper's own validation is the actual security boundary: it never trusts the caller's `Request.Name` field as-is; it always compares against its own independently-computed `deploy.AppcmdPath()`.
- Every `CommandRunner`-shaped type continues to satisfy `deploy.CommandRunner` (`Run(name string, args ...string) ([]byte, error)`) so `deploy.IISSwapper` never changes.
- No real appcmd.exe invocation in any automated test — production execution paths are exercised with fakes; only a manual, human-supervised live session (final step of this plan, not a task) touches the real service.
- `cmd/deploydemo` (the existing standalone live-testing tool) is explicitly out of scope for this plan — it keeps calling `deploy.RealCommandRunner{}` directly, unchanged, since it is already documented as a separate testing-only tool outside the production security boundary.
- Match existing code style: doc comments explain *why*, not *what*; error messages are prefixed with the package name (`"iishelper: ..."`), matching `"deploy: ..."` elsewhere.

---

### Task 1: Request/response protocol and the security-boundary validator

**Files:**
- Create: `backend/internal/iishelper/protocol.go`
- Create: `backend/internal/iishelper/validate.go`
- Create: `backend/internal/iishelper/validate_test.go`
- Modify: `backend/internal/deploy/iisswap.go:31-42,76` (export `appcmdPath` as `AppcmdPath`)
- Modify: `backend/internal/deploy/iisswap_test.go:39` (update the renamed call)

**Interfaces:**
- Produces: `iishelper.Request{Name string, Args []string}`, `iishelper.Response{Output []byte, Error string}`, `iishelper.PipeName` (const), `iishelper.ErrInvalidRequest` (sentinel), `iishelper.ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool) error`
- Produces: `deploy.AppcmdPath() string` (renamed from the unexported `appcmdPath`)
- Consumes: nothing from earlier tasks (this is the first task)

- [ ] **Step 1: Rename `appcmdPath` to the exported `AppcmdPath` in `internal/deploy`**

Edit `backend/internal/deploy/iisswap.go`. Change the doc comment and function signature:

```go
// AppcmdPath returns the absolute path to appcmd.exe. Installing the IIS
// Windows feature does not add appcmd.exe's directory to PATH, so it can't
// be invoked by bare name — it must always be resolved from its fixed,
// well-known system location, %SystemRoot%\System32\inetsrv\appcmd.exe.
//
// Exported so internal/iishelper's request validator can independently
// compute the same path and compare it against an incoming request's Name
// field, rather than trusting that field as-is.
func AppcmdPath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows` // SystemRoot is always set on real Windows machines; this fallback only matters for unusual environments
	}
	return filepath.Join(systemRoot, "System32", "inetsrv", "appcmd.exe")
}
```

Update the one call site in the same file (`SetPhysicalPath`):

```go
	_, err := s.runner.Run(AppcmdPath(), "set", "vdir", siteName+"/", "/physicalPath:"+path)
```

- [ ] **Step 2: Update the renamed call in the existing test**

Edit `backend/internal/deploy/iisswap_test.go:39` — change `appcmdPath()` to `AppcmdPath()`.

- [ ] **Step 3: Run the deploy package tests to confirm the rename didn't break anything**

Run: `cd backend && go test ./internal/deploy/... -run TestSetPhysicalPath -v`
Expected: PASS (same tests as before, just recompiled against the renamed function)

- [ ] **Step 4: Write the protocol types**

Create `backend/internal/iishelper/protocol.go`:

```go
// Package iishelper implements privilege separation for the one operation
// in this codebase that needs Administrator rights: pointing an IIS
// site's physical path at a new release directory via appcmd.exe.
// devplatform.exe (git hosting, panel API, and — critically — running a
// repository's own build scripts) never needs to run elevated; only this
// package's Server does, and it accepts exactly one request shape.
//
// This package is Windows-only: it depends on named pipes
// (github.com/Microsoft/go-winio) and, in cmd/iishelper, the Windows
// Service Control Manager (golang.org/x/sys/windows/svc). DevPlatform
// only ever runs on Windows (IIS has no other platform), so no
// cross-platform fallback is provided.
package iishelper

// PipeName is the well-known named pipe devplatform.exe's
// HelperCommandRunner dials and cmd/iishelper listens on. Fixed and
// unconfigurable — this is not a general-purpose IPC mechanism, it is the
// one channel between exactly these two processes.
const PipeName = `\\.\pipe\devplatform-iishelper`

// Request is what devplatform.exe sends: run Name with Args. Its shape
// mirrors deploy.CommandRunner.Run's parameters, but in practice Name is
// always deploy.AppcmdPath() and Args is always the fixed "set vdir"
// shape ValidateRequest checks — see that function's doc comment for why
// this type stays generic-looking without the request itself being
// treated as trusted.
type Request struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// Response carries the result back. Error is empty on success; Output
// holds appcmd's combined stdout/stderr either way, matching
// deploy.CommandRunner.Run's own (output, error) shape.
type Response struct {
	Output []byte `json:"output"`
	Error  string `json:"error,omitempty"`
}
```

- [ ] **Step 5: Write the failing tests for ValidateRequest**

Create `backend/internal/iishelper/validate_test.go`:

```go
package iishelper

import (
	"errors"
	"testing"
)

const testAppcmdPath = `C:\Windows\System32\inetsrv\appcmd.exe`

func testAllowedSites() map[string]bool {
	return map[string]bool{"DevPlatform Test Site": true}
}

func TestValidateRequest_AcceptsTheOneAllowedShape(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	if err := ValidateRequest(req, testAppcmdPath, testAllowedSites()); err != nil {
		t.Fatalf("expected a valid request to be accepted, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongProgram(t *testing.T) {
	req := Request{
		Name: `C:\Windows\System32\cmd.exe`,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong program, got: %v", err)
	}
}

func TestValidateRequest_RejectsUnknownSite(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "Some Other Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for an unlisted site, got: %v", err)
	}
}

func TestValidateRequest_RejectsRelativePhysicalPath(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", "/physicalPath:releases\\5"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a relative physical path, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongVerb(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"delete", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a wrong verb, got: %v", err)
	}
}

func TestValidateRequest_RejectsWrongArgumentCount(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/"},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a short argument list, got: %v", err)
	}
}

func TestValidateRequest_RejectsSiteArgumentMissingTrailingSlash(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a site argument missing its trailing slash, got: %v", err)
	}
}

func TestValidateRequest_RejectsFourthArgumentMissingPhysicalPathPrefix(t *testing.T) {
	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `C:\inetpub\devplatform-test\releases\5`},
	}
	err := ValidateRequest(req, testAppcmdPath, testAllowedSites())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for a fourth argument missing the /physicalPath: prefix, got: %v", err)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: FAIL — `ValidateRequest`/`ErrInvalidRequest`/`Request` undefined (validate.go doesn't exist yet)

- [ ] **Step 7: Implement ValidateRequest**

Create `backend/internal/iishelper/validate.go`:

```go
package iishelper

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidRequest indicates a Request that does not exactly match the
// one operation this helper is willing to perform. Wrapped with
// fmt.Errorf("%w: ...") in every rejection below so callers can test for
// it with errors.Is regardless of the specific reason.
var ErrInvalidRequest = errors.New("iishelper: request does not match the only allowed operation")

// ValidateRequest is the actual security boundary of this package: it
// never trusts req as coming from a well-behaved devplatform.exe. It
// independently re-derives what a legitimate request must look like —
// appcmdPath is the caller's own computation of deploy.AppcmdPath()
// (passed in rather than imported directly so tests can use a fixed
// value), and allowedSites is the set of IIS site names this deploy
// server is actually configured to manage (see LoadAllowedSites) — and
// rejects anything that deviates in any way from
// appcmd.exe set vdir "<one of allowedSites>/" /physicalPath:<absolute path>
func ValidateRequest(req Request, appcmdPath string, allowedSites map[string]bool) error {
	if req.Name != appcmdPath {
		return fmt.Errorf("%w: unexpected program %q", ErrInvalidRequest, req.Name)
	}
	if len(req.Args) != 4 {
		return fmt.Errorf("%w: expected exactly 4 arguments, got %d", ErrInvalidRequest, len(req.Args))
	}
	if req.Args[0] != "set" || req.Args[1] != "vdir" {
		return fmt.Errorf("%w: unexpected verb %q %q", ErrInvalidRequest, req.Args[0], req.Args[1])
	}

	site, ok := strings.CutSuffix(req.Args[2], "/")
	if !ok {
		return fmt.Errorf("%w: site argument %q must end with /", ErrInvalidRequest, req.Args[2])
	}
	if !allowedSites[site] {
		return fmt.Errorf("%w: %q is not a configured deploy target site", ErrInvalidRequest, site)
	}

	path, ok := strings.CutPrefix(req.Args[3], "/physicalPath:")
	if !ok {
		return fmt.Errorf("%w: fourth argument must start with /physicalPath:", ErrInvalidRequest)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: physical path %q must be absolute", ErrInvalidRequest, path)
	}

	return nil
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: PASS — all 8 `TestValidateRequest_*` tests green

- [ ] **Step 9: Commit**

```bash
cd backend
git add internal/deploy/iisswap.go internal/deploy/iisswap_test.go internal/iishelper/protocol.go internal/iishelper/validate.go internal/iishelper/validate_test.go
git commit -m "feat: add iishelper request protocol and the fixed-shape request validator"
```

---

### Task 2: Allowed-sites loader

**Files:**
- Create: `backend/internal/iishelper/sites.go`
- Create: `backend/internal/iishelper/sites_test.go`

**Interfaces:**
- Consumes: nothing from Task 1
- Produces: `iishelper.LoadAllowedSites(path string) (map[string]bool, error)` — used by Task 6 (cmd/iishelper) and referenced by Task 1's `ValidateRequest` as the shape of `allowedSites`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/iishelper/sites_test.go`:

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

func TestLoadAllowedSites_ReadsSiteNamesFromTheTargetsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "targets.json")
	const contents = `[
		{"repo": "Intranet-F", "environment": "test", "recipe": "npm", "siteName": "Intranet-F Test", "keepVersions": 5},
		{"repo": "Intranet-B", "environment": "test", "recipe": "dotnet", "siteName": "Intranet-B Test", "keepVersions": 5}
	]`
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
	path := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	_, err := LoadAllowedSites(path)
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/iishelper/... -run TestLoadAllowedSites -v`
Expected: FAIL — `LoadAllowedSites` undefined

- [ ] **Step 3: Implement LoadAllowedSites**

Create `backend/internal/iishelper/sites.go`:

```go
package iishelper

import (
	"encoding/json"
	"fmt"
	"os"
)

// targetEntry mirrors only the one field of internal/deployment.Target
// this package cares about. Deliberately not importing internal/deployment
// itself — this package's job (deciding which physical-path changes are
// allowed) is independent of that package's request/approval business
// logic, and duplicating one field name is cheaper than adding a
// dependency between them.
type targetEntry struct {
	SiteName string `json:"siteName"`
}

// LoadAllowedSites reads the same deploy targets file
// internal/deployment.LoadTargets reads (path comes from the same
// DEVPLATFORM_DEPLOY_TARGETS_FILE environment variable) and returns the
// set of SiteName values it declares — the only sites this helper will
// ever agree to repoint. An empty path returns an empty set with no
// error, matching this codebase's established "no targets file
// configured means nothing is deployable" safe default.
func LoadAllowedSites(path string) (map[string]bool, error) {
	sites := map[string]bool{}
	if path == "" {
		return sites, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to read deploy targets file %q: %w", path, err)
	}

	var entries []targetEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("iishelper: failed to parse deploy targets file %q: %w", path, err)
	}

	for _, e := range entries {
		if e.SiteName != "" {
			sites[e.SiteName] = true
		}
	}
	return sites, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: PASS — all Task 1 and Task 2 tests green

- [ ] **Step 5: Commit**

```bash
cd backend
git add internal/iishelper/sites.go internal/iishelper/sites_test.go
git commit -m "feat: load the allowed IIS site list from the deploy targets file"
```

---

### Task 3: Server core (transport-agnostic)

**Files:**
- Create: `backend/internal/iishelper/server.go`
- Create: `backend/internal/iishelper/server_test.go`

**Interfaces:**
- Consumes: `Request`, `Response` (Task 1), `ValidateRequest` (Task 1)
- Produces: `iishelper.Executor` (type `func(name string, args ...string) ([]byte, error)`), `iishelper.Server{AppcmdPath string, AllowedSites map[string]bool, Execute Executor}`, `(*Server).Serve(ln net.Listener) error` — used by Task 6 (cmd/iishelper)

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/iishelper/server_test.go`:

```go
package iishelper

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

// listen opens a loopback TCP listener for the test — the Server's own
// logic (validate, then execute, then respond) doesn't depend on named
// pipes at all, so a plain TCP listener exercises exactly the same code
// path without requiring a real Windows named pipe in tests.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open test listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func roundTrip(t *testing.T, addr string, req Request) Response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial test server: %v", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return resp
}

func TestServer_ExecutesAValidatedRequestAndReturnsItsOutput(t *testing.T) {
	ln := listen(t)
	var gotName string
	var gotArgs []string
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			gotName, gotArgs = name, args
			return []byte("ok"), nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error != "" {
		t.Fatalf("expected no error, got: %s", resp.Error)
	}
	if string(resp.Output) != "ok" {
		t.Fatalf("expected output %q, got %q", "ok", resp.Output)
	}
	if gotName != testAppcmdPath || len(gotArgs) != 4 {
		t.Fatalf("Execute was not called with the validated request: name=%q args=%v", gotName, gotArgs)
	}
}

func TestServer_RejectsAnInvalidRequestWithoutCallingExecute(t *testing.T) {
	ln := listen(t)
	executed := false
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			executed = true
			return nil, nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "Some Unlisted Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error == "" {
		t.Fatal("expected a non-empty error for an invalid request")
	}
	if executed {
		t.Fatal("Execute must never be called for a request that fails validation")
	}
}

func TestServer_ReturnsOutputAlongsideAnExecutionError(t *testing.T) {
	ln := listen(t)
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			return []byte("appcmd exited 5: access denied"), errExecFailed
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error == "" {
		t.Fatal("expected the execution error to be reported")
	}
	if string(resp.Output) != "appcmd exited 5: access denied" {
		t.Fatalf("expected the execution output to be preserved for diagnostics, got %q", resp.Output)
	}
}

func TestServer_HandlesMultipleSequentialConnections(t *testing.T) {
	ln := listen(t)
	calls := 0
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			calls++
			return []byte("ok"), nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	roundTrip(t, ln.Addr().String(), req)
	roundTrip(t, ln.Addr().String(), req)

	if calls != 2 {
		t.Fatalf("expected 2 executions across 2 connections, got %d", calls)
	}
}
```

Add the small helper error used above to `server_test.go` as well (a package-level var in the same file, right after the imports):

```go
var errExecFailed = errors.New("test: simulated execution failure")
```

(add `"errors"` to the import block)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/iishelper/... -run TestServer -v`
Expected: FAIL — `Server`/`Executor` undefined

- [ ] **Step 3: Implement Server**

Create `backend/internal/iishelper/server.go`:

```go
package iishelper

import (
	"encoding/json"
	"log"
	"net"
)

// Executor actually runs a request that has already passed
// ValidateRequest. Production wiring (cmd/iishelper) passes
// deploy.RealCommandRunner{}.Run; tests pass a fake that records calls
// without ever touching a real appcmd.exe.
type Executor func(name string, args ...string) ([]byte, error)

// Server is the transport-agnostic core of iishelper: given any
// net.Listener, it accepts connections, validates each request against
// AppcmdPath/AllowedSites, and only calls Execute for requests that pass.
// Deliberately independent of the Windows-specific named-pipe setup (see
// cmd/iishelper) so this logic — the actual security boundary — is
// testable with a plain loopback TCP listener, no real named pipe or
// Windows Service required.
type Server struct {
	AppcmdPath   string
	AllowedSites map[string]bool
	Execute      Executor
}

// Serve accepts connections from ln until Accept returns an error (e.g.
// ln was closed by the caller during shutdown). Each connection carries
// exactly one request/response pair.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		log.Printf("iishelper: failed to decode request: %v", err)
		return
	}

	resp := s.process(req)
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		log.Printf("iishelper: failed to encode response: %v", err)
	}
}

func (s *Server) process(req Request) Response {
	if err := ValidateRequest(req, s.AppcmdPath, s.AllowedSites); err != nil {
		log.Printf("iishelper: rejected request: %v", err)
		return Response{Error: err.Error()}
	}

	out, err := s.Execute(req.Name, req.Args...)
	if err != nil {
		return Response{Output: out, Error: err.Error()}
	}
	return Response{Output: out}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: PASS — all Task 1, 2, and 3 tests green

- [ ] **Step 5: Commit**

```bash
cd backend
git add internal/iishelper/server.go internal/iishelper/server_test.go
git commit -m "feat: add iishelper's transport-agnostic request server"
```

---

### Task 4: HelperCommandRunner client

**Files:**
- Create: `backend/internal/iishelper/client.go`
- Create: `backend/internal/iishelper/client_test.go`

**Interfaces:**
- Consumes: `Request`, `Response` (Task 1)
- Produces: `iishelper.HelperCommandRunner{Dial func() (net.Conn, error)}`, `(*HelperCommandRunner).Run(name string, args ...string) ([]byte, error)` (satisfies `deploy.CommandRunner`), `iishelper.NewHelperCommandRunner() *HelperCommandRunner` — used by Task 6 (devplatform.exe wiring)

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/iishelper/client_test.go`:

```go
package iishelper

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
)

// fakeHelperServer accepts exactly one connection, decodes one Request,
// and replies with a canned Response — enough to exercise
// HelperCommandRunner's wire format without a real named pipe.
func fakeHelperServer(t *testing.T, resp Response) (addr string, gotReq chan Request) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open fake server listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	gotReq = make(chan Request, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var req Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		gotReq <- req
		json.NewEncoder(conn).Encode(resp)
	}()

	return ln.Addr().String(), gotReq
}

func TestHelperCommandRunner_SendsTheRequestAndReturnsSuccessfulOutput(t *testing.T) {
	addr, gotReq := fakeHelperServer(t, Response{Output: []byte("ok")})
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) { return net.Dial("tcp", addr) }}

	out, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if string(out) != "ok" {
		t.Fatalf("expected output %q, got %q", "ok", out)
	}

	req := <-gotReq
	if req.Name != testAppcmdPath || len(req.Args) != 4 {
		t.Fatalf("unexpected request sent over the wire: %+v", req)
	}
}

func TestHelperCommandRunner_TurnsAResponseErrorIntoAGoError(t *testing.T) {
	addr, _ := fakeHelperServer(t, Response{Error: "iishelper: rejected request"})
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) { return net.Dial("tcp", addr) }}

	_, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err == nil {
		t.Fatal("expected an error when the response carries one")
	}
}

func TestHelperCommandRunner_ReturnsAClearErrorWhenTheHelperIsUnreachable(t *testing.T) {
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) {
		return nil, errors.New("test: connection refused")
	}}

	_, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err == nil {
		t.Fatal("expected an error when Dial fails")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/iishelper/... -run TestHelperCommandRunner -v`
Expected: FAIL — `HelperCommandRunner` undefined

- [ ] **Step 3: Implement HelperCommandRunner**

Create `backend/internal/iishelper/client.go`:

```go
package iishelper

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialTimeout bounds how long a single Run call — connect, send, and
// read the response — is allowed to take. If iishelper is not running or
// hangs, a deploy must fail cleanly here rather than block forever;
// internal/deployment.Handlers.Approve already has its own, longer
// deploy-level timeout, so this is the inner safety net for this one
// step.
const dialTimeout = 30 * time.Second

// HelperCommandRunner implements deploy.CommandRunner by forwarding the
// call to iishelper over a named pipe instead of executing appcmd.exe
// directly — this is the only production change deploy.IISSwapper needed,
// since it already depended only on the CommandRunner interface.
type HelperCommandRunner struct {
	// Dial opens a connection to iishelper. NewHelperCommandRunner sets
	// this to a real named-pipe dial; tests set it directly to dial a
	// loopback listener instead.
	Dial func() (net.Conn, error)
}

// NewHelperCommandRunner returns a HelperCommandRunner that dials
// iishelper's well-known named pipe (PipeName).
func NewHelperCommandRunner() *HelperCommandRunner {
	return &HelperCommandRunner{
		Dial: func() (net.Conn, error) {
			return winio.DialPipe(PipeName, nil)
		},
	}
}

// Run satisfies deploy.CommandRunner. name/args are forwarded verbatim to
// iishelper, which independently validates them before executing
// anything — Run itself does no validation, it is purely a transport.
func (h *HelperCommandRunner) Run(name string, args ...string) ([]byte, error) {
	conn, err := h.Dial()
	if err != nil {
		return nil, fmt.Errorf("iishelper: failed to connect to the IIS helper service: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(dialTimeout))

	if err := json.NewEncoder(conn).Encode(Request{Name: name, Args: args}); err != nil {
		return nil, fmt.Errorf("iishelper: failed to send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("iishelper: failed to read response: %w", err)
	}
	if resp.Error != "" {
		return resp.Output, fmt.Errorf("iishelper: %s", resp.Error)
	}
	return resp.Output, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/iishelper/... -v`
Expected: PASS — all Task 1-4 tests green (note: this step requires `go-winio` to be resolvable; Step 5 below promotes it to a direct dependency)

- [ ] **Step 5: Promote go-winio to a direct dependency and verify the whole module still builds**

Run: `cd backend && go mod tidy && go build ./... && go vet ./...`
Expected: `go.mod`'s `github.com/Microsoft/go-winio` line loses its `// indirect` marker (it was already present transitively via go-git, so no new module version is downloaded); build and vet both clean.

- [ ] **Step 6: Verify deploy.IISSwapper is a valid consumer of HelperCommandRunner (compile-time interface check)**

Add this line near the top of `backend/internal/iishelper/client.go`, right after the type declaration, to catch any signature drift at compile time rather than only at wiring time in Task 6:

```go
var _ interface {
	Run(name string, args ...string) ([]byte, error)
} = (*HelperCommandRunner)(nil)
```

Run: `cd backend && go build ./...`
Expected: clean (this is a compile-time-only check; there's no runtime behavior to test)

- [ ] **Step 7: Commit**

```bash
cd backend
git add internal/iishelper/client.go internal/iishelper/client_test.go go.mod go.sum
git commit -m "feat: add HelperCommandRunner, the pipe-transport CommandRunner implementation"
```

---

### Task 5: cmd/iishelper — the Windows Service binary

**Files:**
- Create: `backend/cmd/iishelper/main.go`
- Create: `backend/cmd/iishelper/main_test.go`

**Interfaces:**
- Consumes: `iishelper.LoadAllowedSites`, `iishelper.Server`, `iishelper.PipeName` (Tasks 2-3), `deploy.AppcmdPath`, `deploy.RealCommandRunner` (Task 1 / existing)
- Produces: the `iishelper.exe` binary; nothing else consumes this package (it's `package main`)

- [ ] **Step 1: Write `setup()`, the part of main that's actually testable**

Create `backend/cmd/iishelper/main.go`:

```go
// Command iishelper is the one process in this platform allowed to run
// appcmd.exe. It runs as a Windows Service (LocalSystem), listens on a
// local named pipe, and only ever executes the one operation
// iishelper.ValidateRequest accepts — see internal/iishelper's package
// doc comment for the full picture.
package main

import (
	"log"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/svc"

	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/iishelper"
)

const serviceName = "DevPlatformIISHelper"

func main() {
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("iishelper: failed to determine execution context: %v", err)
	}

	ln, srv, err := setup()
	if err != nil {
		log.Fatalf("iishelper: setup failed: %v", err)
	}

	if isService {
		if err := svc.Run(serviceName, &windowsService{listener: ln, server: srv}); err != nil {
			log.Fatalf("iishelper: service run failed: %v", err)
		}
		return
	}

	// Not running under the Service Control Manager — e.g. a developer
	// running iishelper.exe directly from a console during testing. Serve
	// until the process is killed.
	log.Printf("iishelper: running interactively on %s (not installed as a Windows Service)", iishelper.PipeName)
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("iishelper: %v", err)
	}
}

// setup loads configuration, opens the named pipe listener, and builds
// the Server that will handle requests on it. Split out from main so
// both the Windows-Service path and the interactive (development) path
// share identical setup, and so it can be tested without needing a real
// Service Control Manager.
//
// DEVPLATFORM_DEPLOY_TARGETS_FILE is read directly via os.Getenv rather
// than through internal/config.Load(), which would pull in config fields
// (SMTP, JWT secret, etc.) this single-purpose binary has no use for.
//
// DEVPLATFORM_IISHELPER_SDDL is an optional Windows security descriptor
// string restricting which account may connect to the named pipe. Left
// empty, go-winio applies its own default pipe security (owner and
// Administrators only) — safe for local development, but production
// should set this explicitly to the one account devplatform.exe runs
// as (see the install script for how to generate this value).
func setup() (net.Listener, *iishelper.Server, error) {
	targetsFile := os.Getenv("DEVPLATFORM_DEPLOY_TARGETS_FILE")
	allowedSites, err := iishelper.LoadAllowedSites(targetsFile)
	if err != nil {
		return nil, nil, err
	}
	log.Printf("iishelper: %d allowed site(s) loaded from %q", len(allowedSites), targetsFile)

	var pipeConfig *winio.PipeConfig
	if sddl := os.Getenv("DEVPLATFORM_IISHELPER_SDDL"); sddl != "" {
		pipeConfig = &winio.PipeConfig{SecurityDescriptor: sddl}
	}
	ln, err := winio.ListenPipe(iishelper.PipeName, pipeConfig)
	if err != nil {
		return nil, nil, err
	}

	srv := &iishelper.Server{
		AppcmdPath:   deploy.AppcmdPath(),
		AllowedSites: allowedSites,
		Execute:      deploy.RealCommandRunner{}.Run,
	}
	return ln, srv, nil
}

// windowsService adapts Server.Serve to the Windows Service Control
// Manager's start/stop/shutdown protocol.
type windowsService struct {
	listener net.Listener
	server   *iishelper.Server
}

func (w *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	done := make(chan error, 1)
	go func() { done <- w.server.Serve(w.listener) }()

	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			log.Printf("iishelper: server stopped unexpectedly: %v", err)
			return false, 1
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				w.listener.Close()
				<-done
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
```

- [ ] **Step 2: Write the failing test for `setup()`**

Create `backend/cmd/iishelper/main_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetup_LoadsSitesAndOpensListener exercises setup()'s wiring without
// ever executing a real appcmd.exe — it only opens the pipe and checks
// the Server it returns is configured correctly; it never calls
// Serve/Execute. If a real iishelper Windows Service happens to already
// be running on the machine this test runs on, opening the same
// well-known pipe name will fail — that's expected and matches how this
// codebase's other live-system tests document their environment
// assumptions (see docs/DURUM.md's dotnet SDK version note).
func TestSetup_LoadsSitesAndOpensListener(t *testing.T) {
	dir := t.TempDir()
	targetsFile := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(targetsFile, []byte(`[{"siteName":"Test Site"}]`), 0o600); err != nil {
		t.Fatalf("failed to write fixture targets file: %v", err)
	}
	t.Setenv("DEVPLATFORM_DEPLOY_TARGETS_FILE", targetsFile)
	t.Setenv("DEVPLATFORM_IISHELPER_SDDL", "")

	ln, srv, err := setup()
	if err != nil {
		t.Fatalf("setup() returned an error: %v", err)
	}
	defer ln.Close()

	if !srv.AllowedSites["Test Site"] {
		t.Errorf("expected %q to be an allowed site, got %v", "Test Site", srv.AllowedSites)
	}
	if srv.AppcmdPath == "" {
		t.Error("expected a non-empty AppcmdPath")
	}
}
```

- [ ] **Step 3: Run the test**

Run: `cd backend && go test ./cmd/iishelper/... -v`
Expected: PASS. (If it fails with a pipe-already-exists error, no other process is expected to be listening on `\\.\pipe\devplatform-iishelper` yet at this point in the plan — check nothing was left running from earlier manual testing.)

- [ ] **Step 4: Full module build/vet/test check**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all packages pass, including the pre-existing ones — this task only added new files.

- [ ] **Step 5: Commit**

```bash
cd backend
git add cmd/iishelper/main.go cmd/iishelper/main_test.go
git commit -m "feat: add the iishelper Windows Service binary"
```

---

### Task 6: Wire devplatform.exe to the helper, and document the one-time install step

**Files:**
- Modify: `backend/cmd/devplatform/main.go:124` (swap `deploy.RealCommandRunner{}` for the helper)
- Create: `backend/cmd/iishelper/install.ps1`
- Modify: `docs/DURUM.md` (record the change, update the two open-decision entries)

**Interfaces:**
- Consumes: `iishelper.NewHelperCommandRunner()` (Task 4)

- [ ] **Step 1: Swap the CommandRunner in devplatform.exe**

Edit `backend/cmd/devplatform/main.go`. Add the import:

```go
	"github.com/kenissha/DevPlatform/backend/internal/iishelper"
```

Change line 124 from:

```go
		deploy.NewIISSwapper(deploy.RealCommandRunner{}),
```

to:

```go
		deploy.NewIISSwapper(iishelper.NewHelperCommandRunner()),
```

- [ ] **Step 2: Verify the whole backend still builds, vets, and tests clean**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: clean. Note: `internal/deployment`'s existing tests use a fake `CommandRunner` directly (never `RealCommandRunner` or the new `HelperCommandRunner`), so they are unaffected by this wiring change — this step exists to catch any compile-time mismatch, not a test regression.

- [ ] **Step 3: Write the one-time install script**

Create `backend/cmd/iishelper/install.ps1`:

```powershell
<#
.SYNOPSIS
  One-time setup for the DevPlatform IIS helper service.

.DESCRIPTION
  Registers iishelper.exe as a Windows Service running as LocalSystem
  (the account Windows services run as by default, which already has the
  rights appcmd.exe needs), and prints the exact environment variable to
  set so the helper's named pipe is restricted to the specific account
  devplatform.exe runs as, instead of accepting connections from any
  local account.

  Run this from an elevated (Administrator) PowerShell prompt.

.PARAMETER ExePath
  Full path to the built iishelper.exe.

.PARAMETER DevPlatformAccount
  The Windows account devplatform.exe runs as, e.g. ".\devplatform-svc"
  or "DOMAIN\devplatform-svc". Used only to print the SDDL string below —
  it is not applied automatically, since DEVPLATFORM_IISHELPER_SDDL is an
  environment variable the operator sets on the service, the same way
  every other DEVPLATFORM_* secret/config value already works in this
  project.
#>
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$DevPlatformAccount
)

New-Service -Name "DevPlatformIISHelper" `
    -BinaryPathName $ExePath `
    -DisplayName "DevPlatform IIS Helper" `
    -Description "Runs appcmd.exe on behalf of devplatform.exe. Do not run devplatform.exe itself elevated - only this service needs Administrator rights." `
    -StartupType Automatic

$sid = (New-Object System.Security.Principal.NTAccount($DevPlatformAccount)).Translate([System.Security.Principal.SecurityIdentifier]).Value

Write-Host ""
Write-Host "Service 'DevPlatformIISHelper' registered (LocalSystem, automatic start)."
Write-Host ""
Write-Host "Before starting it, restrict the named pipe to $DevPlatformAccount by setting this"
Write-Host "environment variable on the DevPlatformIISHelper service (System Properties >"
Write-Host "Environment Variables, or 'setx' for a machine-wide value, then restart the service):"
Write-Host ""
Write-Host "  DEVPLATFORM_IISHELPER_SDDL = D:P(A;;GA;;;$sid)"
Write-Host ""
Write-Host "Then: Start-Service DevPlatformIISHelper"
```

- [ ] **Step 4: Update docs/DURUM.md**

Edit `docs/DURUM.md`. Find the line containing exactly `## Sıradaki iş` and insert this new subsection immediately before it (i.e. right after the end of the security-review update block that precedes it):

```markdown
**2026-08-13 güncelleme — IIS yardımcı servisi (yetki ayrımı):**
`internal/iishelper` ve `cmd/iishelper` eklendi. `devplatform.exe` artık
appcmd.exe'yi hiç doğrudan çalıştırmıyor — appcmd'yi çalıştıran tek şey,
ayrı, küçük bir Windows Service (`iishelper`), `LocalSystem` hesabıyla
çalışıyor ve yerel bir named pipe üzerinden sadece tek bir işlemi kabul
ediyor: bilinen bir IIS site'ının fiziksel yolunu mutlak bir dizine
çevirmek. Gelen her istek bu tek şekle tam uymuyorsa reddediliyor —
çağıranın (`devplatform.exe`) gönderdiği appcmd yoluna güvenilmiyor,
servis kendi yolunu bağımsızca hesaplayıp karşılaştırıyor.

Sonuç: `devplatform.exe` artık hiçbir zaman Administrator yetkisiyle
çalışmasına gerek yok — repo'nun kendi build script'i (`npm run build`/
`dotnet publish`) her zaman olduğu gibi çalışıyor ama artık Admin
yetkisiyle değil. Sadece `iishelper` servisi (dar, sabit, tek işlemli)
yükseltilmiş yetkiyle çalışıyor.

Kurulum: `backend/cmd/iishelper/install.ps1`, bir kere elevated
PowerShell'den çalıştırılır, servisi kaydeder ve named pipe'ı
`devplatform.exe`'nin çalıştığı hesaba kısıtlayacak `DEVPLATFORM_IISHELPER_SDDL`
değerini üretip ekrana yazar — bu değeri servisin ortam değişkenlerine
elle eklemek gerekiyor (diğer `DEVPLATFORM_*` gizli değerleri gibi).

**Henüz yapılmadı:** gerçek servis kurulup, mevcut test IIS site'ına
karşı uçtan uca canlı doğrulama (bkz. plan dosyasının sonundaki
"gözetimli doğrulama" adımları) — bu, orijinal IIS kanıtlamasında
yapıldığı gibi birlikte, elle yapılacak.
```

Then find this exact block inside "## Bilinmesi gereken kararlar":

```markdown
- **Açık karar — build adımı Administrator yetkili hesapla çalışıyor
  (2026-08-13):** `deploy.Pipeline`, appcmd'nin çalışabilmesi için
  Administrator yetkisi gereken bir hesapla `npm run build`/`dotnet
  publish`'i çalıştırıyor — yani repoya push edip deploy onayı alabilen
  biri, host üzerinde fiilen Admin RCE'ye ulaşabiliyor. CI sistemlerinde
  genel bir sorun (bu projeye özgü bir hata değil), ama gerçek IIS'e
  bağlanmadan önce bilinçli bir karar gerektiriyor: ayrı, düşük yetkili
  bir build hesabı mı, yoksa sadece IIS swap'ını yapan ayrı yükseltilmiş
  bir yardımcı süreç mi.
```

and replace it with:

```markdown
- **Çözüldü — build adımı artık Administrator yetkisiyle çalışmıyor
  (2026-08-13):** bkz. yukarıdaki "IIS yardımcı servisi" güncellemesi.
  `deploy.Pipeline`'ın build adımı hâlâ `devplatform.exe` içinde
  çalışıyor ama artık asla yükseltilmiş yetkiyle değil; appcmd'yi
  çalıştıran tek şey ayrı, dar yetkili `iishelper` servisi. Kalan tek
  gerçek iş: gerçek sunucuda servisi kurup canlı doğrulamak (kod değil,
  ops adımı).
```

- [ ] **Step 5: Commit**

```bash
cd backend
git add cmd/devplatform/main.go cmd/iishelper/install.ps1
git commit -m "feat: point devplatform.exe at the iishelper service instead of running appcmd directly"
cd ..
git add docs/DURUM.md
git commit -m "docs: record the iishelper privilege-separation change and close the open build-privilege decision"
```

---

## After this plan: manual, human-supervised verification (not a task — do not automate)

This mirrors how the original IIS proof-of-concept was verified (`cmd/deploydemo` against a real, throwaway IIS site) — do this together, live, before considering the feature done:

1. Build both binaries: `go build -o iishelper.exe ./cmd/iishelper` and the existing `devplatform.exe` build.
2. From an elevated PowerShell prompt, run `install.ps1` against the real account devplatform.exe will run as on this machine, then start the service.
3. Run `devplatform.exe` **not elevated** (a normal, non-Administrator console) and confirm it starts successfully.
4. Trigger a real deploy approval against the existing throwaway `DevPlatform Test Site` and confirm the swap still works end to end — this proves the split didn't just move the privilege requirement, it actually removed it from devplatform.exe.
5. Stop the `DevPlatformIISHelper` service and confirm a deploy attempt now fails with the "helper unreachable" error instead of hanging or silently succeeding.
