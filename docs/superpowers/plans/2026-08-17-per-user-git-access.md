# Kişi Başına Git Erişimi Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace DevPlatform's single shared git username/password
(`DEVPLATFORM_GIT_USERNAME`/`_PASSWORD`, `internal/gitauth`) with a
per-person git credential, so `internal/access`'s per-repo restriction is
finally enforced at the git protocol level, not just in the panel API.

**Architecture:** A new package, `internal/gittoken`, stores only the
SHA-256 hash of each person's single active token (the raw value is
returned exactly once, at generation time, and never persisted). A new
`RequireTokenAndAccess` middleware replaces `gitauth.RequireBasicAuth` in
front of the `/git/` route: it authenticates the Basic Auth
username/token pair against `gittoken.Store`, extracts the repo name from
the URL, and reuses `access.Store.CanAccess` — the exact same function
the panel API already calls — so there is no second, parallel
authorization system to keep in sync. Two new admin-adjacent endpoints
(`POST /api/me/git-token`, `DELETE /api/git-token/{subject}`) and one new
frontend page (`Hesabım`) let a person generate their own key and let an
admin revoke someone else's.

**Tech Stack:** Go 1.22+ (`net/http`, `crypto/rand`, `crypto/sha256`,
`crypto/subtle`), React + TypeScript (existing `frontend/src` conventions).

**Spec:** `docs/superpowers/specs/2026-08-17-per-user-git-access-design.md`

## Global Constraints

- Raw tokens are **never** persisted — only a SHA-256 hash. (Spec: "Ham
  anahtar hiçbir zaman diskte durmuyor".)
- One active token per subject; generating a new one invalidates the
  previous one immediately. No named/multiple keys. (Spec: "Kapsam Dışı".)
- No read/write split and no SSH support — git access mirrors the panel's
  `access.Store.CanAccess` exactly, nothing finer-grained. (Spec: "Kapsam
  Dışı".)
- Token comparison uses `subtle.ConstantTimeCompare`, matching the
  discipline already in `internal/gitauth` (both comparisons evaluated
  unconditionally — no early-exit that would leak timing information
  about whether a subject exists).
- Storage is a single JSON file (`<DataDir>/git-tokens.json`) written via
  the project's existing temp-file-then-rename atomic pattern (see
  `internal/access/access.go`'s `save`) — no database, matching every
  other `internal/*` store in this codebase.
- `internal/gitauth` and `DEVPLATFORM_GIT_USERNAME`/`_PASSWORD` are
  **fully removed**, not deprecated in place — the spec is explicit that
  a transition period defeats the point ("paylaşılan şifre kalırsa
  düzeltmenin anlamı kalmıyor").
- `POST /api/me/git-token` takes no path parameter — it always acts on
  the caller's own JWT subject, so nobody can mint a token for someone
  else through it.

---

### Task 1: `internal/gittoken.Store`

**Files:**
- Create: `backend/internal/gittoken/store.go`
- Test: `backend/internal/gittoken/store_test.go`

**Interfaces:**
- Produces: `gittoken.NewStore(path string) *Store`,
  `(*Store).Generate(subject string) (string, error)`,
  `(*Store).Revoke(subject string) error`,
  `(*Store).Verify(subject, token string) bool`,
  `gittoken.ErrInvalidSubject error`. Tasks 2 and 3 depend on these exact
  signatures.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/gittoken/store_test.go`:

```go
package gittoken

import "testing"

func TestGenerate_ProducesVerifiableToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	token, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if token == "" {
		t.Fatal("Generate returned an empty token")
	}
	if !store.Verify("dev-1", token) {
		t.Error("Verify(subject, correct token) = false, want true")
	}
}

func TestGenerate_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if _, err := store.Generate(""); err != ErrInvalidSubject {
		t.Errorf("Generate(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestGenerate_RegeneratingInvalidatesThePreviousToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	first, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	second, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if first == second {
		t.Fatal("two calls to Generate produced the same token")
	}
	if store.Verify("dev-1", first) {
		t.Error("Verify still accepts the token that Generate replaced")
	}
	if !store.Verify("dev-1", second) {
		t.Error("Verify rejects the current token")
	}
}

func TestVerify_RejectsWrongToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, err := store.Generate("dev-1"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if store.Verify("dev-1", "not-the-real-token") {
		t.Error("Verify accepted a wrong token")
	}
}

func TestVerify_RejectsUnknownSubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if store.Verify("nobody", "any-token") {
		t.Error("Verify accepted a subject with no stored token")
	}
}

func TestRevoke_InvalidatesTheToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := store.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if err := store.Revoke("dev-1"); err != nil {
		t.Fatalf("Revoke returned error: %v", err)
	}
	if store.Verify("dev-1", token) {
		t.Error("Verify still accepts a token after Revoke")
	}
}

func TestRevoke_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("nobody"); err != nil {
		t.Errorf("Revoke on a subject with no token returned error: %v", err)
	}
}

func TestRevoke_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke(""); err != ErrInvalidSubject {
		t.Errorf("Revoke(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-tokens.json"
	store1 := NewStore(path)
	token, err := store1.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	store2 := NewStore(path)
	if !store2.Verify("dev-1", token) {
		t.Error("a fresh Store instance backed by the same file does not see the earlier Generate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: FAIL — package `gittoken` does not exist yet (no `store.go`).

- [ ] **Step 3: Write the implementation**

Create `backend/internal/gittoken/store.go`:

```go
// Package gittoken implements per-user credentials for the git
// smart-HTTP endpoints, replacing the single shared
// DEVPLATFORM_GIT_USERNAME/_PASSWORD pair — see
// docs/superpowers/specs/2026-08-17-per-user-git-access-design.md. Each
// person gets at most one active, high-entropy token; only its SHA-256
// hash is ever persisted, and the raw value is returned exactly once, at
// generation time.
package gittoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrInvalidSubject = errors.New("gittoken: subject must not be empty")

// tokenBytes is the raw entropy of a generated token before base64
// encoding — 32 bytes (256 bits), the same budget internal/auth's JWT
// secret and internal/secretsvault's key use elsewhere in this codebase.
const tokenBytes = 32

// unknownSubjectHash is compared against when subject has no stored
// hash, so Verify always runs ConstantTimeCompare against a same-length
// buffer regardless of whether subject exists — an unknown-subject
// rejection and a wrong-token rejection take the same amount of time,
// the same discipline internal/gitauth (which this package replaces)
// already applied to username/password comparison.
var unknownSubjectHash = hex.EncodeToString(make([]byte, sha256.Size))

// Store persists, per subject, the SHA-256 hash of their single active
// git token. Unlike internal/access, every caller here already has a
// concrete Store (see cmd/devplatform/main.go) — there is no
// "optionally inert nil Store" case to support.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Generate creates a new random token for subject, persists its hash
// (overwriting any previous token — a subject has at most one active
// token, the same "generating a new one invalidates the old" model as a
// password reset), and returns the raw token. This is the only moment
// the raw value exists outside the caller's memory; it is never stored
// and cannot be recovered afterward.
func (s *Store) Generate(subject string) (string, error) {
	if subject == "" {
		return "", ErrInvalidSubject
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return "", err
	}
	registry[subject] = hash(token)
	if err := s.save(registry); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke removes subject's stored token hash, if any. A subject with no
// stored token is not an error — revoking is idempotent, the same
// "removing something already absent succeeds" convention
// internal/access.Store.Clear uses.
func (s *Store) Revoke(subject string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	delete(registry, subject)
	return s.save(registry)
}

// Verify reports whether token is subject's current active token. Load
// errors are treated as "not verified" rather than surfaced — this runs
// on the hot path of every git request as an HTTP Basic Auth gate, where
// the only two outcomes that matter are "allowed" or "401"; a corrupt
// store file must fail closed, not 500.
func (s *Store) Verify(subject, token string) bool {
	s.mu.Lock()
	registry, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return false
	}

	want, ok := registry[subject]
	if !ok {
		want = unknownSubjectHash
	}
	got := hash(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// load reads the registry. A missing file is an empty registry, not an
// error — nobody has generated a token yet.
func (s *Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	registry := map[string]string{}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}
	return registry, nil
}

// save writes the registry via a temp file and rename, so an interrupted
// write can't leave a half-written registry behind — the same pattern
// internal/access.Store.save and internal/users.Store.save use.
func (s *Store) save(registry map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".git-tokens-*.tmp")
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: PASS (all 9 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gittoken/store.go backend/internal/gittoken/store_test.go
git commit -m "feat(gittoken): add per-user git token store"
```

---

### Task 2: `RequireTokenAndAccess` middleware

**Files:**
- Create: `backend/internal/gittoken/middleware.go`
- Test: `backend/internal/gittoken/middleware_test.go`

**Interfaces:**
- Consumes: `gittoken.NewStore`/`Generate`/`Verify` (Task 1);
  `access.NewStore(path string) *access.Store`,
  `(*access.Store).Set(subject string, repos []string) error`,
  `(*access.Store).CanAccess(subject, repo string) (bool, error)`
  (`backend/internal/access/access.go`); `users.NewStore(path string) *users.Store`,
  `(*users.Store).Upsert(subject, email, role string) (User, error)`,
  `(*users.Store).Get(subject string) (User, bool, error)` where
  `User.Role string` (`backend/internal/users/users.go`);
  `gitserver.Prefix = "/git"` (`backend/internal/gitserver/gitserver.go`);
  `auth.RoleAdmin Role = "admin"` (`backend/internal/auth/auth.go`).
- Produces: `gittoken.RequireTokenAndAccess(tokens *Store, accessStore *access.Store, usersStore *users.Store, next http.Handler) http.Handler`.
  Task 4 wires this in as the direct replacement for
  `gitauth.RequireBasicAuth(...)` in `cmd/devplatform/main.go`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/gittoken/middleware_test.go`:

```go
package gittoken

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

func stubGitHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func newTestRequest(repo, subject, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/git/"+repo+".git/info/refs", nil)
	if subject != "" || token != "" {
		req.SetBasicAuth(subject, token)
	}
	return req
}

func TestRequireTokenAndAccess_RejectsMissingCredentials(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

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

func TestRequireTokenAndAccess_RejectsInvalidToken(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	if _, err := tokens.Generate("dev-1"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("repo", "dev-1", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireTokenAndAccess_AllowsAnUnrestrictedUser(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_BlocksARestrictedUserFromAnUngrantedRepo(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"intranet-frontend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireTokenAndAccess_AllowsARestrictedUserTheirGrantedRepo(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_AdminBypassesRestriction(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("admin-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	if err := accessStore.Set("admin-1", []string{"some-other-repo"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	if _, err := usersStore.Upsert("admin-1", "admin@example.com", "admin"); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := newTestRequest("intranet-backend", "admin-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (admins always bypass)", rec.Code, http.StatusOK)
	}
}

func TestRequireTokenAndAccess_RejectsPathWithNoRepoName(t *testing.T) {
	tokens := NewStore(t.TempDir() + "/git-tokens.json")
	token, err := tokens.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	accessStore := access.NewStore(t.TempDir() + "/access.json")
	usersStore := users.NewStore(t.TempDir() + "/users.json")
	handler := RequireTokenAndAccess(tokens, accessStore, usersStore, stubGitHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/", nil)
	req.SetBasicAuth("dev-1", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRepoNameFromPath(t *testing.T) {
	cases := []struct {
		path     string
		wantRepo string
		wantOK   bool
	}{
		{"/git/intranet-backend.git/info/refs", "intranet-backend", true},
		{"/git/intranet-backend.git/git-upload-pack", "intranet-backend", true},
		{"/git/intranet-backend.git", "intranet-backend", true},
		{"/api/repos", "", false},
		{"/git/", "", false},
	}
	for _, c := range cases {
		repo, ok := repoNameFromPath(c.path)
		if repo != c.wantRepo || ok != c.wantOK {
			t.Errorf("repoNameFromPath(%q) = (%q, %v), want (%q, %v)", c.path, repo, ok, c.wantRepo, c.wantOK)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: FAIL — `RequireTokenAndAccess` and `repoNameFromPath` are undefined.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/gittoken/middleware.go`:

```go
package gittoken

import (
	"net/http"
	"strings"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// RequireTokenAndAccess wraps next (the git smart-HTTP handler) with:
// authentication against tokens (HTTP Basic Auth, username = subject,
// password = raw token — what a `git` client sends), then the exact same
// per-repo authorization the panel API already uses
// (access.Store.CanAccess), extracting the repo name from the request
// path. Admins bypass the repo check, mirroring
// access.RequireRepoAccess's own admin-bypass rule — but since a git
// Basic Auth request carries no role claim (unlike a panel JWT), the
// role has to be looked up in usersStore instead. A subject who has
// never used the panel (so has no usersStore entry yet) is simply
// treated as non-admin here; that only matters if they're also
// repo-restricted, since an unrestricted subject passes the access check
// either way.
func RequireTokenAndAccess(tokens *Store, accessStore *access.Store, usersStore *users.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, token, ok := r.BasicAuth()
		if !ok || !tokens.Verify(subject, token) {
			unauthorized(w)
			return
		}

		repo, ok := repoNameFromPath(r.URL.Path)
		if !ok {
			http.Error(w, "400 Bad Request", http.StatusBadRequest)
			return
		}

		if !isAdmin(usersStore, subject) {
			allowed, err := accessStore.CanAccess(subject, repo)
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, "403 Forbidden", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// repoNameFromPath extracts "foo" from a git smart-HTTP request path like
// "/git/foo.git/info/refs" (gitserver.Prefix + "/" + name + ".git" +
// anything). Repo names are restricted elsewhere (repostore) to
// [a-zA-Z0-9_-]+, so the first ".git" occurrence is always the real
// boundary — no path-traversal concern, since the result is only ever
// used as a comparison key into access.Store, never as a filesystem path.
func repoNameFromPath(path string) (repo string, ok bool) {
	rest, ok := strings.CutPrefix(path, gitserver.Prefix+"/")
	if !ok {
		return "", false
	}
	repo, _, found := strings.Cut(rest, ".git")
	if !found || repo == "" {
		return "", false
	}
	return repo, true
}

func isAdmin(usersStore *users.Store, subject string) bool {
	u, ok, err := usersStore.Get(subject)
	if err != nil || !ok {
		return false
	}
	return u.Role == string(auth.RoleAdmin)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="DevPlatform Git"`)
	http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: PASS (all tests in the package, Task 1's and Task 2's).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gittoken/middleware.go backend/internal/gittoken/middleware_test.go
git commit -m "feat(gittoken): add RequireTokenAndAccess middleware"
```

---

### Task 3: `internal/gittoken.Handlers`

**Files:**
- Create: `backend/internal/gittoken/handlers.go`
- Test: `backend/internal/gittoken/handlers_test.go`

**Interfaces:**
- Consumes: `gittoken.NewStore`/`Generate`/`Verify` (Task 1);
  `auth.UserFromContext(ctx) (*auth.User, bool)` where
  `auth.User.Subject string` (`backend/internal/auth/auth.go`);
  `auth.RequireAuth(secret []byte, next http.Handler) http.Handler`.
- Produces: `gittoken.Handlers{ Store *Store }` with methods
  `GenerateMine(w http.ResponseWriter, r *http.Request)` and
  `Revoke(w http.ResponseWriter, r *http.Request)`. Task 4 mounts these
  as `POST /api/me/git-token` and `DELETE /api/git-token/{subject}`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/gittoken/handlers_test.go`:

```go
package gittoken

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

const testJWTSecret = "test-secret"

func signTestToken(t *testing.T, subject, role string) string {
	t.Helper()
	c := jwt.MapClaims{
		"sub":  subject,
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

func TestHandlers_GenerateMine_ReturnsATokenForTheAuthenticatedCaller(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/me/git-token", authMW(http.HandlerFunc(h.GenerateMine)))

	req := httptest.NewRequest(http.MethodPost, "/api/me/git-token", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Token == "" {
		t.Fatal("response did not include a token")
	}
	if !h.Store.Verify("dev-1", body.Token) {
		t.Error("returned token does not verify against the store")
	}
}

func TestHandlers_GenerateMine_RejectsUnauthenticatedRequests(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/me/git-token", authMW(http.HandlerFunc(h.GenerateMine)))

	req := httptest.NewRequest(http.MethodPost, "/api/me/git-token", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlers_Revoke_InvalidatesTheSubjectsToken(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	token, err := h.Store.Generate("dev-1")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("DELETE /api/git-token/{subject}", http.HandlerFunc(h.Revoke))

	req := httptest.NewRequest(http.MethodDelete, "/api/git-token/dev-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if h.Store.Verify("dev-1", token) {
		t.Error("token still verifies after Revoke")
	}
}

func TestHandlers_Revoke_NonexistentSubjectStillSucceeds(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	mux := http.NewServeMux()
	mux.Handle("DELETE /api/git-token/{subject}", http.HandlerFunc(h.Revoke))

	req := httptest.NewRequest(http.MethodDelete, "/api/git-token/nobody", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: FAIL — `Handlers`, `GenerateMine`, `Revoke` are undefined.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/gittoken/handlers.go`:

```go
package gittoken

import (
	"encoding/json"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
)

// Handlers exposes Store's operations as http.HandlerFuncs — see
// cmd/devplatform/main.go and internal/server for how they're mounted.
type Handlers struct {
	Store *Store
}

// GenerateMine handles POST /api/me/git-token, meant to be mounted
// behind auth.RequireAuth only (no role requirement — anyone can mint
// their own key). It always acts on the caller's own JWT subject; there
// is deliberately no path parameter, so nobody can request a token on
// someone else's behalf through this endpoint.
func (h *Handlers) GenerateMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := h.Store.Generate(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// Revoke handles DELETE /api/git-token/{subject}, meant to be mounted
// behind auth.RequireRole(auth.RoleAdmin, ...) — it revokes someone
// else's token, mirroring internal/access.Handlers.Clear's admin-only
// pattern for /api/access/{subject}.
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Revoke(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: PASS (all tests in the package — Tasks 1, 2, and 3 combined).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gittoken/handlers.go backend/internal/gittoken/handlers_test.go
git commit -m "feat(gittoken): add GenerateMine/Revoke HTTP handlers"
```

---

### Task 4: Wire `gittoken` in, remove `gitauth` entirely

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/devplatform/main.go`
- Modify: `backend/internal/server/server.go`
- Modify: `backend/internal/gitserver/gitserver.go` (doc comments only —
  they name `gitauth` by symbol, which no longer exists after this task)
- Delete: `backend/internal/gitauth/gitauth.go`
- Delete: `backend/internal/gitauth/gitauth_test.go`
- Modify: `docs/DURUM.md`

**Interfaces:**
- Consumes: everything produced by Tasks 1-3
  (`gittoken.NewStore`, `gittoken.RequireTokenAndAccess`,
  `gittoken.Handlers`).
- Produces: `server.Deps.GitTokens *gittoken.Handlers` field, routes
  `POST /api/me/git-token` and `DELETE /api/git-token/{subject}` live in
  `internal/server.NewRouter`. No later task consumes these directly —
  Task 5's frontend calls them over HTTP.

- [ ] **Step 1: Remove `GitUsername`/`GitPassword` from `config.Config`**

In `backend/internal/config/config.go`, remove the two fields from the
struct:

```go
type Config struct {
	ListenAddr   string
	DataDir      string
	JWTSecret    string
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
```

Remove the doc-comment paragraph that describes them (the second
paragraph of `Load`'s doc comment, right after the "falling back to
development-friendly defaults" line):

```go
// Load reads configuration from the environment, falling back to
// development-friendly defaults when a variable is unset.
//
// JWTSecret's "dev-not-a-real-secret" default is the same kind of
```

And remove the two `getEnv` calls from the `Load` function's returned
struct literal:

```go
	return Config{
		ListenAddr:        listenAddr(),
		DataDir:           getEnv("DEVPLATFORM_DATA_DIR", "./data"),
		JWTSecret:         getEnv("DEVPLATFORM_JWT_SECRET", "dev-not-a-real-secret"),
```

- [ ] **Step 2: Delete the now-obsolete config test**

In `backend/internal/config/config_test.go`, delete this entire test
function (it asserts on the two fields just removed):

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

- [ ] **Step 3: Run the config package tests**

Run: `cd backend && go test ./internal/config/... -v`
Expected: PASS (the deleted test is gone; every remaining test in the
file is untouched and still compiles, since none of them reference
`GitUsername`/`GitPassword`).

- [ ] **Step 4: Delete `internal/gitauth`**

Delete both files:
- `backend/internal/gitauth/gitauth.go`
- `backend/internal/gitauth/gitauth_test.go`

- [ ] **Step 5: Update `gitserver.go`'s stale doc-comment references**

In `backend/internal/gitserver/gitserver.go`, `NewHandler`'s doc comment
currently reads:

```go
// The returned handler is wrapped with withReceivePackAuthShim, a
// permanent go-git v6-alpha workaround (see that function's doc comment)
// — it stays regardless of DevPlatform's own auth, since this constructor
// has no guarantee callers wrap it with gitauth.RequireBasicAuth (this
// package's own tests call it directly, unwrapped).
```

Change `gitauth.RequireBasicAuth` to `gittoken.RequireTokenAndAccess`:

```go
// The returned handler is wrapped with withReceivePackAuthShim, a
// permanent go-git v6-alpha workaround (see that function's doc comment)
// — it stays regardless of DevPlatform's own auth, since this constructor
// has no guarantee callers wrap it with gittoken.RequireTokenAndAccess
// (this package's own tests call it directly, unwrapped).
```

And `withReceivePackAuthShim`'s doc comment currently reads:

```go
// This is unrelated to DevPlatform's own auth (internal/gitauth), which
// wraps this handler from the outside in main.go and rejects unauthorized
// requests before they ever reach here — by the time a request reaches
// this shim through that path, it already carries a real, validated
// Authorization header, so the synthetic header below is never applied in
// production. It only fires for callers that invoke NewHandler directly
// without gitauth in front (this package's own integration tests), where
```

Change both `internal/gitauth`/`gitauth` mentions to
`internal/gittoken`/`gittoken`:

```go
// This is unrelated to DevPlatform's own auth (internal/gittoken), which
// wraps this handler from the outside in main.go and rejects unauthorized
// requests before they ever reach here — by the time a request reaches
// this shim through that path, it already carries a real, validated
// Authorization header, so the synthetic header below is never applied in
// production. It only fires for callers that invoke NewHandler directly
// without gittoken in front (this package's own integration tests), where
```

(The rest of that comment paragraph is unchanged.)

- [ ] **Step 6: Add `GitTokens` to `server.Deps` and mount its routes**

In `backend/internal/server/server.go`, add the import:

```go
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/gittoken"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
```

Add a field to `Deps`, right after `Access`:

```go
	// Access controls per-person repository visibility (see internal/access,
	// Faz 3's "proje bazlı yetkilendirme"). Optional: a nil Store means
	// nobody is restricted, matching the platform's behavior before this
	// existed. When set, every repo-scoped route below additionally
	// requires access.RequireRepoAccess to pass, and /api/access exposes
	// the admin-only management API.
	Access *access.Store
	// GitTokens issues and revokes the per-person git credentials that
	// gate the /git/ route (see internal/gittoken). Not optional in
	// practice — cmd/devplatform/main.go always constructs one — but
	// nil-checking it here would only mask a wiring bug at startup.
	GitTokens *gittoken.Handlers
}
```

In `NewRouter`, after `accessHandlers := &access.Handlers{Store: deps.Access}`:

```go
	accessHandlers := &access.Handlers{Store: deps.Access}
	gitTokens := deps.GitTokens
```

Add two routes, right after the `/api/access/{subject}` trio at the end
of the route list (before `return mux`):

```go
	mux.Handle("GET /api/access", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.List))))
	mux.Handle("PUT /api/access/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.Set))))
	mux.Handle("DELETE /api/access/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(accessHandlers.Clear))))

	// Per-person git credentials (see internal/gittoken). Anyone can mint
	// their own (no {subject} in the path — it always targets the
	// caller's own JWT subject); only an Admin can revoke someone else's,
	// the same split /api/access uses between "sees own" and "manages
	// everyone".
	mux.Handle("POST /api/me/git-token", authMiddleware(http.HandlerFunc(gitTokens.GenerateMine)))
	mux.Handle("DELETE /api/git-token/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(gitTokens.Revoke))))

	return mux
```

- [ ] **Step 7: Wire it all up in `main.go`**

In `backend/cmd/devplatform/main.go`, remove the `gitauth` import and add
`gittoken` in alphabetical order:

```go
	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/deployment"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/gittoken"
	"github.com/kenissha/DevPlatform/backend/internal/iishelper"
```

Replace the old handler construction:

```go
	gitHandler := gitserver.NewHandler(cfg.DataDir)
	authedGitHandler := gitauth.RequireBasicAuth(cfg.GitUsername, cfg.GitPassword, gitHandler)
```

with just:

```go
	gitHandler := gitserver.NewHandler(cfg.DataDir)
```

`gittoken.RequireTokenAndAccess` needs `accessStore` and `usersStore`,
which aren't constructed yet at that point in the file — move the
`authedGitHandler` construction down to right after `accessStore` is
built. Change:

```go
	usersStore := users.NewStore(filepath.Join(cfg.DataDir, "users.json"))
	// accessStore starts with nobody restricted — every repo stays visible
	// to everyone exactly as before this existed, until an admin calls
	// PUT /api/access/{subject} for a specific person (see
	// internal/access's doc comment for why unrestricted is the default).
	accessStore := access.NewStore(filepath.Join(cfg.DataDir, "access.json"))
```

to:

```go
	usersStore := users.NewStore(filepath.Join(cfg.DataDir, "users.json"))
	// accessStore starts with nobody restricted — every repo stays visible
	// to everyone exactly as before this existed, until an admin calls
	// PUT /api/access/{subject} for a specific person (see
	// internal/access's doc comment for why unrestricted is the default).
	accessStore := access.NewStore(filepath.Join(cfg.DataDir, "access.json"))
	// gitTokenStore holds the per-person git credentials that replace the
	// single shared DEVPLATFORM_GIT_USERNAME/_PASSWORD pair — see
	// docs/superpowers/specs/2026-08-17-per-user-git-access-design.md.
	gitTokenStore := gittoken.NewStore(filepath.Join(cfg.DataDir, "git-tokens.json"))
	authedGitHandler := gittoken.RequireTokenAndAccess(gitTokenStore, accessStore, usersStore, gitHandler)
```

Construct the handlers alongside the other `*Handlers` structs. Change:

```go
	repoHandlers := &repoapi.Handlers{Repos: store, Audit: auditLogger, Access: accessStore}
```

to:

```go
	repoHandlers := &repoapi.Handlers{Repos: store, Audit: auditLogger, Access: accessStore}
	gitTokenHandlers := &gittoken.Handlers{Store: gitTokenStore}
```

Finally, pass it into `server.Deps`. Change:

```go
	router := server.NewRouter(server.Deps{
		GitHandler:     authedGitHandler,
		AuthMiddleware: authMiddleware,
		MergeRequests:  mrHandlers,
		Repos:          repoHandlers,
		Tasks:          taskHandlers,
		Stats:          statsHandlers,
		Audit:          auditHandlers,
		Notifications:  notifyHandlers,
		Deployments:    deploymentHandlers,
		Users:          usersStore,
		Access:         accessStore,
	})
```

to:

```go
	router := server.NewRouter(server.Deps{
		GitHandler:     authedGitHandler,
		AuthMiddleware: authMiddleware,
		MergeRequests:  mrHandlers,
		Repos:          repoHandlers,
		Tasks:          taskHandlers,
		Stats:          statsHandlers,
		Audit:          auditHandlers,
		Notifications:  notifyHandlers,
		Deployments:    deploymentHandlers,
		Users:          usersStore,
		Access:         accessStore,
		GitTokens:      gitTokenHandlers,
	})
```

- [ ] **Step 8: Build, vet, and run the full backend test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: builds clean, vets clean, all tests PASS (the compiler will
catch any leftover `gitauth`/`GitUsername`/`GitPassword` reference in
this step if Steps 1-7 missed one).

- [ ] **Step 9: Update `docs/DURUM.md`**

The file currently has an open decision recorded in "Bilinmesi gereken
kararlar" (around line 354). Replace this bullet:

```markdown
- **Açık karar — git erişimi tek paylaşılan kimlik bilgisiyle çalışıyor
  (2026-08-13):** `internal/access`'in proje bazlı kısıtlaması sadece
  panel API'lerinde geçerli. `git clone`/`push`, `DEVPLATFORM_GIT_USERNAME`/
  `_PASSWORD` ile herkes için aynı tek kullanıcı adı/şifreyi kullanıyor
  (`gitauth.RequireBasicAuth`, `/git/` rotası `authMiddleware`'in de
  `RequireRepoAccess`'in de dışında). Yani panelden birini bir repoya
  kısıtlasan bile, o kişi paylaşılan git kimlik bilgileriyle her repoyu
  doğrudan klonlayıp push'layabiliyor — bu, projenin "ikinci mühendisi tam
  erişim vermeden işe alma" amacını bu haliyle geçersiz kılıyor. Gerçek
  çözüm kullanıcı bazlı git kimlik doğrulama (örn. kişi başına token)
  gerektiriyor — bu boyutta bir iş, ayrı bir brainstorm+plan hak ediyor.
  Karar bekliyor: şimdi mi ele alınsın, yoksa ikinci mühendis işe alınana
  kadar bilinen bir sınırlama olarak mı bırakılsın.
```

with:

```markdown
- **Çözüldü — git artık kişi başına anahtarla çalışıyor (2026-08-17):**
  `internal/gitauth`'ın tek paylaşılan `DEVPLATFORM_GIT_USERNAME`/
  `_PASSWORD` çifti tamamen kaldırıldı (geçiş dönemi yok). Yeni
  `internal/gittoken`, kişi başına tek bir anahtarın SHA-256 hash'ini
  saklıyor; ham anahtar hiç diskte durmuyor, sadece üretildiği an bir
  kere gösteriliyor (panelde "Hesabım" sayfası, `POST /api/me/git-token`).
  `/git/` rotasının önündeki `RequireTokenAndAccess` ara katmanı, panelin
  zaten kullandığı `access.Store.CanAccess`'in **aynısını** çağırıyor —
  git için ayrı bir yetki sistemi yok. Ayrıntı için
  `docs/superpowers/specs/2026-08-17-per-user-git-access-design.md`.
  Paylaşılan şifreyle git kullanan biri varsa (şimdiye kadar sadece biz),
  bu değişiklikten sonra Hesabım sayfasından yeni bir anahtar üretmesi
  gerekiyor — eski paylaşılan şifre artık hiçbir yerde geçerli değil.
```

- [ ] **Step 10: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go \
        backend/cmd/devplatform/main.go backend/internal/server/server.go \
        backend/internal/gitserver/gitserver.go docs/DURUM.md
git rm backend/internal/gitauth/gitauth.go backend/internal/gitauth/gitauth_test.go
git commit -m "feat(gittoken): wire per-user git tokens in, remove shared gitauth credential"
```

---

### Task 5: Frontend — "Hesabım" page + API client

**Files:**
- Create: `frontend/src/pages/HesabimPage.tsx`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/AppLayout.tsx`
- Modify: `frontend/src/components/icons.tsx`

**Interfaces:**
- Consumes: `POST /api/me/git-token` and `DELETE /api/git-token/{subject}`
  (Task 4); `useAuth()` from `frontend/src/auth/AuthContext` (existing,
  returns `{ user: User | null, ... }` where `User.subject: string`, per
  `frontend/src/api/types.ts`).
- Produces: `api.generateGitToken(): Promise<{ token: string }>`,
  `api.revokeGitToken(subject: string): Promise<void>` in
  `frontend/src/api/client.ts`. Task 6 consumes `api.revokeGitToken`.

- [ ] **Step 1: Add the two API client methods**

In `frontend/src/api/client.ts`, add after the existing `clearAccess`
entry (still inside the `api` object, before its closing `}`):

```ts
  clearAccess: (subject: string) =>
    request<void>(`/api/access/${encodeURIComponent(subject)}`, { method: 'DELETE' }),

  // Per-person git credential (Admin-only revoke; anyone can mint their
  // own — see backend/internal/gittoken). The raw token in
  // generateGitToken's response is shown to the caller exactly once;
  // DevPlatform never stores or re-displays it.
  generateGitToken: () => request<{ token: string }>('/api/me/git-token', { method: 'POST' }),
  revokeGitToken: (subject: string) =>
    request<void>(`/api/git-token/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
}
```

- [ ] **Step 2: Add the `KeyIcon` glyph**

In `frontend/src/components/icons.tsx`, append after `LockIcon`'s
closing brace (end of file):

```tsx

export function KeyIcon({ className }: IconProps) {
  return (
    <svg className={className} {...base} aria-hidden="true">
      <circle cx="5" cy="10" r="2.5" />
      <path d="M7 8.5L13 2.5" />
      <path d="M10.5 5L12.5 7" />
    </svg>
  )
}
```

- [ ] **Step 3: Create `HesabimPage.tsx`**

Create `frontend/src/pages/HesabimPage.tsx`:

```tsx
import { useState } from 'react'
import { api, ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

// HesabimPage lets the signed-in person mint their own git credential —
// see docs/superpowers/specs/2026-08-17-per-user-git-access-design.md.
// The raw token is shown exactly once, right after generation; it is
// never stored or re-displayed — only its SHA-256 hash persists on the
// server (see backend/internal/gittoken).
export function HesabimPage() {
  const { user } = useAuth()
  const [token, setToken] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  async function generate() {
    setGenerating(true)
    setError(null)
    setCopied(false)
    try {
      const res = await api.generateGitToken()
      setToken(res.token)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Anahtar oluşturulamadı')
    } finally {
      setGenerating(false)
    }
  }

  async function copy() {
    if (!token) return
    await navigator.clipboard.writeText(token)
    setCopied(true)
  }

  const subject = user?.subject ?? ''
  const cloneExample = `git clone http://${subject}:${token ?? '<anahtar>'}@${window.location.host}/git/<repo>.git`

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Hesabım</h1>
          <p className="page-subtitle">Git üzerinden clone/push yapmak için kendi kişisel anahtarınız</p>
        </div>
      </div>

      <div className="card">
        <p>
          Git'e paylaşılan bir şifreyle değil, kendinize ait bir anahtarla bağlanırsınız. Panelde
          hangi repolara erişebiliyorsanız git'te de aynı repolara erişebilirsiniz — ayrı bir izin
          sistemi yok.
        </p>

        <div className="form-actions">
          <button type="button" className="btn-primary" disabled={generating} onClick={generate}>
            {token ? 'Yeni anahtar oluştur (eskisini geçersiz kılar)' : 'Anahtar oluştur'}
          </button>
        </div>

        {error && <p className="error">{error}</p>}

        {token && (
          <div className="field">
            <label>Anahtarınız</label>
            <p className="error">
              Bu anahtar bir daha gösterilmeyecek — şimdi bir yere kaydedin.
            </p>
            <textarea
              readOnly
              rows={2}
              value={token}
              spellCheck={false}
              onFocus={(e) => e.currentTarget.select()}
            />
            <div className="form-actions">
              <button type="button" className="btn-ghost" onClick={copy}>
                {copied ? 'Kopyalandı' : 'Kopyala'}
              </button>
            </div>
            <label>Örnek kullanım</label>
            <textarea readOnly rows={2} value={cloneExample} spellCheck={false} />
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Wire the route**

In `frontend/src/App.tsx`, add the import after `DashboardPage`'s:

```ts
import { DashboardPage } from './pages/DashboardPage'
import { HesabimPage } from './pages/HesabimPage'
import { LoginPage } from './pages/LoginPage'
```

Add the route after `/access`:

```tsx
              <Route path="/access" element={<AccessPage />} />
              <Route path="/hesabim" element={<HesabimPage />} />
              <Route path="/notifications" element={<NotificationsPage />} />
```

- [ ] **Step 5: Add the nav link**

In `frontend/src/components/AppLayout.tsx`, add `KeyIcon` to the icons
import (alphabetical, between `DeployIcon` and `LockIcon`):

```ts
import {
  AuditIcon,
  BellIcon,
  BranchIcon,
  ChartIcon,
  DeployIcon,
  KeyIcon,
  LockIcon,
  LogoMark,
  MergeIcon,
  OverviewIcon,
  RepoIcon,
  TaskIcon,
} from './icons'
```

Add the nav item after "Bildirimler" and before the admin-only "Proje
erişimi" item — everyone manages their own key, so this one is not
role-gated:

```tsx
              <li>
                <NavLink end to="/notifications" className={navClass}>
                  <BellIcon />
                  <span className="nav-label">Bildirimler</span>
                  {unreadCount > 0 && <span className="nav-count">{unreadCount}</span>}
                </NavLink>
              </li>
              <li>
                <NavLink end to="/hesabim" className={navClass}>
                  <KeyIcon />
                  <span className="nav-label">Hesabım</span>
                </NavLink>
              </li>
              {user?.role === 'admin' && (
```

- [ ] **Step 6: Build and lint**

Run: `cd frontend && npm run build && npm run lint`
Expected: build succeeds, lint clean (this catches unused-import and
type errors across all of Steps 1-5).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/HesabimPage.tsx frontend/src/api/client.ts \
        frontend/src/App.tsx frontend/src/components/AppLayout.tsx \
        frontend/src/components/icons.tsx
git commit -m "feat(frontend): add Hesabım page for self-service git tokens"
```

---

### Task 6: Frontend — revoke button on `AccessPage`

**Files:**
- Modify: `frontend/src/pages/AccessPage.tsx`

**Interfaces:**
- Consumes: `api.revokeGitToken(subject: string): Promise<void>` (Task 5).

- [ ] **Step 1: Add the revoke button component and wire it into the row**

In `frontend/src/pages/AccessPage.tsx`, add a new component after
`AccessPage`'s closing brace and before `function AccessEditor(...)`:

```tsx
function GitTokenRevokeButton({ subject }: { subject: string }) {
  const [revoking, setRevoking] = useState(false)
  const [message, setMessage] = useState<string | null>(null)

  async function revoke() {
    setRevoking(true)
    setMessage(null)
    try {
      await api.revokeGitToken(subject)
      setMessage('İptal edildi')
    } catch (err) {
      setMessage(err instanceof ApiError ? err.message : 'İptal edilemedi')
    } finally {
      setRevoking(false)
    }
  }

  return (
    <>
      <button type="button" className="btn-ghost" disabled={revoking} onClick={revoke}>
        Git anahtarını iptal et
      </button>
      {message && <span className="muted">{message}</span>}
    </>
  )
}
```

Insert it into each row's `row-main`, before the existing "Düzenle"
button:

```tsx
                  <div className="row-main">
                    <LockIcon className="muted" />
                    <span className="row-title">{person.email || person.subject}</span>
                    <span className="spacer" />
                    <span className={`badge ${restricted ? 'badge-accent' : 'badge-neutral'}`}>
                      {restricted ? `${allowed.length} repo` : 'Tüm repolar'}
                    </span>
                    <GitTokenRevokeButton subject={person.subject} />
                    <button type="button" className="btn-ghost" onClick={() => setOpenSubject(isOpen ? null : person.subject)}>
                      {isOpen ? 'Kapat' : 'Düzenle'}
                    </button>
                  </div>
```

- [ ] **Step 2: Build and lint**

Run: `cd frontend && npm run build && npm run lint`
Expected: build succeeds, lint clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/AccessPage.tsx
git commit -m "feat(frontend): let admins revoke a person's git token from Proje erişimi"
```
