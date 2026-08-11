# Push-Time Secret Scanning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject an entire push, before any of its content is written to disk, if any new blob it introduces matches a known secret pattern (private key, AWS key, JWT, .NET connection-string password) — closing the "push anında secret taraması" item named as deferred in the previous plan.

**Architecture:** Extends `backend/internal/gitserver`'s existing decorator pattern from the branch-protection plan with a second, independent decorator: `scanningLoader`/`scanningStorer`, composed alongside `protectingLoader`/`protectedStorer` in `NewHandler`. Uses the already-built, standalone `backend/internal/secretscan` package (`Scan(content []byte) (name string, ok bool)`) as the pattern-matching engine — this task does not touch that package's detection logic beyond fixing one broken test fixture.

**Critical architectural finding from planning (read before implementing):** the natural first guess — override `storage.Storer.SetEncodedObject` to inspect objects as they're written — does **not** work. Reading go-git v6's actual receive-pack source during planning showed the real call chain is `transport.ReceivePack` → `packfile.UpdateObjectStorage` → (since the real filesystem storer implements the optional `storer.PackfileWriter` fast path) `packfile.NewParser(...).Parse()` → `storage.RawObjectWriter(typ, sz)` **per object**, never `SetEncodedObject`. A decorator overriding the wrong method would compile, pass a naive smoke test, and scan nothing in production. This plan's decorator overrides `RawObjectWriter`, confirmed against the actual parser source.

A second finding, also confirmed by reading source rather than assumed: this plan's decorator pattern (embedding `storage.Storer` as an **interface** field, the same pattern the branch-protection plan already established) happens to prevent the `PackfileWriter` fast-path bypass entirely, because Go only promotes methods declared on the embedded interface's method set — not extra methods the concrete underlying type happens to also implement. This is why `RawObjectWriter` gets called at all instead of the raw-packfile-copy shortcut. This is load-bearing, not incidental — Task 2 includes an explicit code comment on this, and a future refactor that embeds a concrete storer type instead of the interface would silently break scanning.

**Tech Stack:** Go 1.22+, existing `backend/internal/secretscan` package, `github.com/go-git/go-git/v6` (already a pinned dependency), the system `git` CLI as the integration-test oracle (same standard as the previous plan).

## Global Constraints

- **Correctness bar is the real `git` CLI**, exactly as in the previous plan: every test that exercises the scanning path must shell out to the actual `git` binary against a running `httptest.Server`, not just call Go-level types.
- **A detected secret rejects the entire push**, not just the offending file or commit — confirmed with the project owner during planning. The rejected content must never be persisted to the server's disk at all, not even transiently (the buffer-then-scan-then-commit design in Task 2 exists specifically for this).
- Only `plumbing.BlobObject`-typed writes are scanned. Commit, tree, and tag objects are passed through untouched — secrets live in file content, not git's own structural metadata, and scanning those object types would be pure overhead.
- Blob content above `1 MiB` (`maxScannedBlobSize`, defined in Task 2) is passed through **unscanned**, not rejected and not buffered — secrets are textual and small; buffering an unbounded blob (e.g. a large binary asset someone pushes) in memory per push is a real resource risk with no corresponding security benefit.
- Security: never echo the matched secret text itself anywhere — not in error messages, not in logs, not in test assertions beyond what's needed to prove a match occurred. Only the pattern's *name* (e.g. `"aws-secret-access-key"`) may appear in rejection messages. This constraint already shaped `secretscan.go`'s existing design (see its `pattern.name` doc comment) — Task 2 must preserve it in the gitserver-side rejection error too.
- Commit after every task; each commit must leave `go build ./...` and `go test ./...` passing. Comments in English; commit messages Conventional-Commits-ish (`feat:`/`fix:`/`test:`).
- This plan does not touch `backend/internal/gitserver/protectedloader.go` (branch protection) — the two decorators are independent and composed side by side in `NewHandler`, not merged into one type.

---

### Task 1: Fix the existing secretscan test-fixture bug

**Files:**
- Modify: `backend/internal/secretscan/secretscan_test.go`

**Interfaces:**
- Consumes: `secretscan.Scan(content []byte) (name string, ok bool)` (already exists, unchanged).
- Produces: no interface change — this task only fixes test data.

- [ ] **Step 1: Reproduce the failure**

Run: `go test ./internal/secretscan/... -v` from `backend/`.
Expected: `TestScan_DetectsKnownPatterns/aws_secret_access_key` FAILs with something like `Scan("aws_secret_access_key = wJalrXUtnFEMIfakeSECRETfakeKEYfakeEXAMPLE") = ok=false, want match "aws-secret-access-key"`.

- [ ] **Step 2: Understand and fix the root cause**

Open `backend/internal/secretscan/secretscan_test.go` and find the `aws_secret_access_key` test case. Its fixture value, `wJalrXUtnFEMIfakeSECRETfakeKEYfakeEXAMPLE`, is 41 characters long. The detector's regex (`backend/internal/secretscan/secretscan.go`) requires exactly 40 base64-alphabet characters after `aws_secret_access_key = ` — matching AWS's real secret-key format, which genuinely is 40 characters. The fixture is wrong, not the regex: fix the test data, don't loosen the pattern.

Replace the fixture with AWS's own long-published example secret key (used in official AWS documentation and every AWS SDK tutorial for exactly this reason — it's a widely known placeholder, not a real credential, and it's exactly 40 characters):
```go
content: "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
```

- [ ] **Step 3: Verify the fix**

Run: `go test ./internal/secretscan/... -v`
Expected: PASS, all cases in `TestScan_DetectsKnownPatterns` and `TestScan_IgnoresBenignContent` green.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/secretscan/secretscan_test.go
git commit -m "fix: correct malformed AWS secret key fixture in secretscan tests"
```

---

### Task 2: Wire secret scanning into the git push path

**Files:**
- Create: `backend/internal/gitserver/scanningloader.go`
- Test: `backend/internal/gitserver/scanningloader_test.go`
- Modify: `backend/internal/gitserver/gitserver.go`

**Interfaces:**
- Consumes: `secretscan.Scan(content []byte) (name string, ok bool)` (Task 1, already fixed). Consumes `transport.Loader` (the same interface `protectingLoader` from the previous plan wraps).
- Produces: `gitserver.NewHandler`'s behavior changes (not its signature) — pushes containing a blob matching a known secret pattern are now rejected server-side. No other package depends on new exported symbols from this task; like branch protection, this is an internal wrapping detail.

- [ ] **Step 1: Write the failing integration tests**

Create `backend/internal/gitserver/scanningloader_test.go`:
```go
package gitserver

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

func TestPush_ContainingSecret_IsRejected(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("secrets"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-with-secret")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	secretContent := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"
	if err := os.WriteFile(filepath.Join(work, "config.txt"), []byte(secretContent), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "config.txt")
	runGit(t, work, "commit", "-m", "accidentally commit a secret")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/secrets.git")

	cmd := exec.Command("git", "push", "origin", "feature-with-secret")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected push containing a secret to be rejected, but it succeeded. Output:\n%s", out)
	}
	t.Logf("push containing a secret correctly failed with:\n%s", out)

	// The real assertion: the branch must not exist on the server at all
	// afterward — not just that the client's push command failed.
	verifyDir := t.TempDir()
	cloneTarget := filepath.Join(verifyDir, "secrets")
	cmd = exec.Command("git", "clone", srv.URL+"/secrets.git", cloneTarget)
	cmd.Dir = verifyDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to clone for verification: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/feature-with-secret")
	cmd.Dir = cloneTarget
	if err := cmd.Run(); err == nil {
		t.Fatal("refs/heads/feature-with-secret exists on the server, but the push containing a secret should have been rejected entirely")
	}
}

func TestPush_WithoutSecret_StillSucceeds(t *testing.T) {
	requireGit(t)

	dataDir := t.TempDir()
	store := repostore.New(dataDir)
	if _, err := store.Create("clean"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}

	srv := httptest.NewServer(NewHandler(dataDir))
	defer srv.Close()

	work := t.TempDir()
	runGit(t, work, "init", "-b", "feature-clean")
	runGit(t, work, "config", "user.email", "test@example.com")
	runGit(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("just some ordinary prose, nothing secret here\n"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "ordinary commit")
	runGit(t, work, "remote", "add", "origin", srv.URL+"/clean.git")
	runGit(t, work, "push", "origin", "feature-clean")
	// runGit already fails the test via t.Fatalf if this push is rejected.
}
```
`requireGit` and `runGit` are already defined in this package's `gitserver_test.go` (from the previous plan) and are reusable here without redefinition, same-package convention already established.

- [ ] **Step 2: Run the tests to verify the first one fails (no scanning yet)**

Run: `go test ./internal/gitserver/... -run TestPush_ContainingSecret_IsRejected -v`
Expected: FAIL — the push currently succeeds (no scanning exists yet), so `t.Fatalf("expected push ... to be rejected, but it succeeded")` triggers.

- [ ] **Step 3: Implement the scanning loader/storer**

Create `backend/internal/gitserver/scanningloader.go`:
```go
package gitserver

import (
	"bytes"
	"fmt"
	"io"
	"net/url"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"

	"github.com/kenissha/DevPlatform/backend/internal/secretscan"
)

// maxScannedBlobSize caps how much of a single blob's content is buffered
// for secret scanning. Secrets are textual and small; blobs larger than
// this are passed through unscanned rather than buffered in memory —
// buffering an unbounded blob (e.g. a large binary asset) in RAM per push
// is a real resource risk with no corresponding security benefit, since
// secrets essentially never hide inside multi-megabyte binaries.
const maxScannedBlobSize = 1 << 20 // 1 MiB

// scanningLoader wraps a transport.Loader so every storer it returns
// scans new blob content for known secret patterns before accepting it.
type scanningLoader struct {
	inner transport.Loader
}

func newScanningLoader(inner transport.Loader) transport.Loader {
	return &scanningLoader{inner: inner}
}

func (l *scanningLoader) Load(u *url.URL) (storage.Storer, error) {
	st, err := l.inner.Load(u)
	if err != nil {
		return nil, err
	}
	return &scanningStorer{Storer: st}, nil
}

// scanningStorer embeds a real storage.Storer so every method is delegated
// automatically via Go interface embedding, except RawObjectWriter, which
// is overridden to scan new blob content before it's committed to storage.
//
// RawObjectWriter, not SetEncodedObject, is the method that matters here:
// go-git v6's packfile parser calls storage.RawObjectWriter per object
// while unpacking an incoming push (confirmed by reading the parser's
// source during planning — see the design/plan docs). A decorator that
// only overrode SetEncodedObject would compile and pass a naive test, but
// would never see real push content in production.
//
// This also depends on embedding storage.Storer as an *interface* field
// (matching protectedStorer's existing pattern in protectedloader.go), not
// the concrete filesystem storer type. The concrete filesystem storer also
// implements the optional storer.PackfileWriter fast path, which writes an
// incoming packfile's raw bytes straight to disk without decoding
// individual objects — bypassing per-object inspection entirely. Since
// that method isn't declared on the storage.Storer interface, embedding
// the interface (not the concrete type) means it isn't promoted here, so
// go-git falls back to the per-object path instead. This is load-bearing:
// if a future refactor embeds a concrete storer type here instead of the
// interface, scanning silently stops running.
type scanningStorer struct {
	storage.Storer
}

func (s *scanningStorer) RawObjectWriter(typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	real, err := s.Storer.RawObjectWriter(typ, sz)
	if err != nil {
		return nil, err
	}
	if typ != plumbing.BlobObject || sz > maxScannedBlobSize {
		return real, nil
	}
	return &scanningWriteCloser{real: real}, nil
}

// scanningWriteCloser buffers a blob's content and scans it on Close,
// before ever forwarding a byte to the real writer — flagged content
// never reaches disk, even partially.
type scanningWriteCloser struct {
	real io.WriteCloser
	buf  bytes.Buffer
}

func (w *scanningWriteCloser) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *scanningWriteCloser) Close() error {
	if name, ok := secretscan.Scan(w.buf.Bytes()); ok {
		return fmt.Errorf("gitserver: push rejected, content matches known secret pattern %q", name)
	}
	if _, err := w.real.Write(w.buf.Bytes()); err != nil {
		return err
	}
	return w.real.Close()
}
```

- [ ] **Step 4: Wire the scanning loader into NewHandler**

Modify `backend/internal/gitserver/gitserver.go` — compose `scanningLoader` alongside the existing `protectingLoader`:
```go
func NewHandler(dataDir string) http.Handler {
	loader := transport.NewFilesystemLoader(osfs.New(dataDir), false)
	protected := newProtectingLoader(loader)
	scanned := newScanningLoader(protected)
	b := backend.New(scanned)
	b.Prefix = Prefix
	return withReceivePackAuthShim(b)
}
```
The two decorators are independent (one guards reference writes, the other guards object writes) and compose in either order — this ordering (`protected` wrapped by `scanned`) is arbitrary but must actually nest both, not replace one with the other.

- [ ] **Step 5: Run the scanning tests**

Run: `go test ./internal/gitserver/... -v`
Expected: PASS — both new tests (`TestPush_ContainingSecret_IsRejected`, `TestPush_WithoutSecret_StillSucceeds`) and every pre-existing test in this package (branch protection, clone/push round-trip) — scanning must not interfere with content that contains no secrets.

If `TestPush_ContainingSecret_IsRejected` fails specifically at the server-side verification step (branch exists when it shouldn't) rather than at the `git push` command itself, that's a real finding — it would mean the server accepted and partially persisted content it should have rejected outright. Do not weaken the test. Investigate whether `RawObjectWriter` is actually being called for this object (add a temporary print/log if needed to confirm), and report exactly what you find, including BLOCKED if you can't resolve it — this is the core property this task exists to deliver.

- [ ] **Step 6: Full build and test**

Run: `go build ./...` and `go test ./...` from `backend/`.
Expected: both succeed, full suite green (config, repostore, server, gitauth, gitserver, secretscan).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/gitserver/scanningloader.go backend/internal/gitserver/scanningloader_test.go backend/internal/gitserver/gitserver.go
git commit -m "feat: reject pushes containing known secret patterns at the storer level"
```

- [ ] **Step 8: Push**

```bash
git push origin main
```

---

## Self-Review Notes

- **Spec coverage:** Delivers the "push anında secret taraması" item explicitly named and deferred in the previous plan's design-doc update. Detection logic (patterns, benign-content exclusions) was already built and is out of this plan's scope beyond the one test-fixture fix in Task 1 — this plan is entirely about correctly wiring an already-correct detector into the actual push path, which research showed was non-trivial (wrong hook point would have shipped silently broken).
- **Placeholder scan:** No TBD/TODO. The architectural finding about `RawObjectWriter` vs `SetEncodedObject` and the `PackfileWriter`-bypass dependency is written out in full in both the plan header and the code comment — not asserted without justification.
- **Type consistency:** `scanningLoader`/`scanningStorer` follow the exact same shape as `protectingLoader`/`protectedStorer` from the previous plan (a `transport.Loader` wrapper whose `Load` wraps the returned `storage.Storer`) — verified consistent naming and structure so a future reader recognizes the pattern instantly. `gitserver.NewHandler`'s signature is unchanged; only its internals gain one more layer, exactly as branch protection did in the previous plan's Task 4.
- **Security:** the core property — a secret is never persisted, not even transiently — is enforced by buffering before any write to the real storer, not by scanning-then-deleting-if-flagged (which would leave a window where flagged content briefly exists on disk). The 1 MiB scan cap is a deliberate, stated tradeoff (resource safety over exhaustive coverage of implausible huge-blob secrets), not an oversight. Matched secret text is never included in error messages or logs, matching the existing `secretscan` package's own stated design constraint.
