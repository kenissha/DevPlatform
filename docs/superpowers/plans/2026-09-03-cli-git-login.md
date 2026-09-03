# CLI Git Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace manual git-token copy-paste with a terminal-based login: a new `devplatform-login` CLI implements git's credential-helper protocol, chains Intranet-B's existing login + devplatform-sso endpoints to mint a DevPlatform git token automatically, and caches it locally (DPAPI-encrypted) so `git pull`/`push` never asks again — and self-heals if a token is ever revoked.

**Architecture:** Two mostly-independent tracks that meet at the end. Track A (backend + panel): `gittoken.Store` moves from "one active token per person" to "any number of independently-revocable tokens per person" — this alone removes the "someone regenerated it and mine silently died" failure mode this session hit repeatedly. Track B (new CLI): `backend/cmd/devplatform-login`, a single Windows binary implementing `get`/`store`/`erase` per git's credential-helper contract, backed by a DPAPI-encrypted local cache and a 3-call login chain (Intranet-B login → Intranet-B devplatform-sso → DevPlatform's own, now-multi-token, `/api/me/git-token`).

**Tech Stack:** Go (backend, existing codebase), `golang.org/x/sys/windows` (DPAPI — already a dependency), `golang.org/x/term` (masked password prompt — **new** dependency, verified compiling on this machine during design), React/TypeScript (Hesabım page).

**Spec:** `docs/superpowers/specs/2026-09-03-cli-git-login-design.md`

## Global Constraints

- No changes to Intranet-B — its `POST /api/auth/login` and
  `POST /api/auth/devplatform-sso` endpoints are used exactly as they
  exist today (verified: plain JSON in/out, no CSRF/CAPTCHA).
- `devplatform-login` is Windows-only, matching this codebase's existing
  `iishelper`/`secretsctl` tools — no cross-platform build concerns.
- The AD password is held only in memory during the login chain, never
  written to disk in any form.
- The local credential cache is DPAPI-encrypted with `CURRENT_USER`
  scope (`windows.CRYPTPROTECT_UI_FORBIDDEN`) — unreadable by another
  Windows account or if copied to another machine.
- `Store.Generate` must never invalidate any other token — this is the
  core behavior change the whole plan exists to make happen.
- The existing admin-only `DELETE /api/git-token/{subject}` must keep
  its "cut off this person's git access entirely" meaning — under the
  new model that means revoking **every** token that subject has.

---

### Task 1: `gittoken.Store` — multi-token data model

**Files:**
- Modify: `backend/internal/gittoken/store.go`
- Modify: `backend/internal/gittoken/store_test.go` (full replace — the
  existing `TestGenerate_RegeneratingInvalidatesThePreviousToken` test
  asserts the exact behavior this task removes)

**Interfaces:**
- Produces: `gittoken.Token{ID, Hash, Label, CreatedAt}`,
  `gittoken.TokenInfo{ID, Label, CreatedAt}` (no Hash — safe to expose),
  `(*Store).Generate(subject, label string) (id, rawToken string, err error)`,
  `(*Store).List(subject string) ([]TokenInfo, error)`,
  `(*Store).Revoke(subject, id string) error`,
  `(*Store).RevokeAll(subject string) error`,
  `(*Store).Verify(subject, token string) bool` (unchanged signature,
  now checks all of subject's tokens).

- [ ] **Step 1: Replace the test file**

Replace the entire contents of `backend/internal/gittoken/store_test.go`:

```go
package gittoken

import "testing"

func TestGenerate_ProducesVerifiableToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	_, token, err := store.Generate("dev-1", "test label")
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

	if _, _, err := store.Generate("", "label"); err != ErrInvalidSubject {
		t.Errorf("Generate(\"\", ...) error = %v, want ErrInvalidSubject", err)
	}
}

func TestGenerate_DoesNotInvalidateAPreviousToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	_, first, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, second, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}
	if first == second {
		t.Fatal("two calls to Generate produced the same token")
	}
	if !store.Verify("dev-1", first) {
		t.Error("Verify rejects the first token after a second was generated — Generate must not invalidate previous tokens")
	}
	if !store.Verify("dev-1", second) {
		t.Error("Verify rejects the second token")
	}
}

func TestVerify_RejectsWrongToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, _, err := store.Generate("dev-1", "label"); err != nil {
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

func TestList_ReturnsTokensNewestFirstWithoutHashes(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	if _, _, err := store.Generate("dev-1", "laptop"); err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	if _, _, err := store.Generate("dev-1", "desktop"); err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	list, err := store.List("dev-1")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d tokens, want 2", len(list))
	}
	if list[0].Label != "desktop" || list[1].Label != "laptop" {
		t.Errorf("labels in order = [%q, %q], want [desktop, laptop] (newest first)", list[0].Label, list[1].Label)
	}
}

func TestList_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if _, err := store.List(""); err != ErrInvalidSubject {
		t.Errorf("List(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestRevoke_InvalidatesOnlyTheMatchingToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	id1, token1, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, token2, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	if err := store.Revoke("dev-1", id1); err != nil {
		t.Fatalf("Revoke returned error: %v", err)
	}
	if store.Verify("dev-1", token1) {
		t.Error("Verify still accepts the revoked token")
	}
	if !store.Verify("dev-1", token2) {
		t.Error("Revoke invalidated a token it wasn't asked to")
	}
}

func TestRevoke_NonexistentIDIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("dev-1", "no-such-id"); err != nil {
		t.Errorf("Revoke on an unknown id returned error: %v", err)
	}
}

func TestRevoke_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.Revoke("", "some-id"); err != ErrInvalidSubject {
		t.Errorf("Revoke(\"\", ...) error = %v, want ErrInvalidSubject", err)
	}
}

func TestRevokeAll_InvalidatesEveryToken(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")
	_, token1, err := store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("first Generate returned error: %v", err)
	}
	_, token2, err := store.Generate("dev-1", "desktop")
	if err != nil {
		t.Fatalf("second Generate returned error: %v", err)
	}

	if err := store.RevokeAll("dev-1"); err != nil {
		t.Fatalf("RevokeAll returned error: %v", err)
	}
	if store.Verify("dev-1", token1) || store.Verify("dev-1", token2) {
		t.Error("RevokeAll left at least one token still valid")
	}
}

func TestRevokeAll_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.RevokeAll("nobody"); err != nil {
		t.Errorf("RevokeAll on a subject with no tokens returned error: %v", err)
	}
}

func TestRevokeAll_RejectsEmptySubject(t *testing.T) {
	store := NewStore(t.TempDir() + "/git-tokens.json")

	if err := store.RevokeAll(""); err != ErrInvalidSubject {
		t.Errorf("RevokeAll(\"\") error = %v, want ErrInvalidSubject", err)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := t.TempDir() + "/git-tokens.json"
	store1 := NewStore(path)
	_, token, err := store1.Generate("dev-1", "laptop")
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
Expected: FAIL — compile errors (`Generate` called with 2 args against
the old 1-arg signature, `List`/`RevokeAll` undefined, etc.).

- [ ] **Step 3: Replace the implementation**

Replace the entire contents of `backend/internal/gittoken/store.go`:

```go
// Package gittoken implements per-user credentials for the git
// smart-HTTP endpoints, replacing the single shared
// DEVPLATFORM_GIT_USERNAME/_PASSWORD pair — see
// docs/superpowers/specs/2026-08-17-per-user-git-access-design.md. Each
// person can have any number of active, independently-revocable
// tokens (one per machine/CLI login is the expected pattern — see
// docs/superpowers/specs/2026-09-03-cli-git-login-design.md) — only
// each token's SHA-256 hash is ever persisted, and a raw value is
// returned exactly once, at generation time.
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
	"time"
)

var ErrInvalidSubject = errors.New("gittoken: subject must not be empty")

// tokenBytes is the raw entropy of a generated token before base64
// encoding — 32 bytes (256 bits), the same budget internal/auth's JWT
// secret and internal/secretsvault's key use elsewhere in this codebase.
const tokenBytes = 32

// idBytes is the raw entropy of a token's ID — this only needs to be
// unique per subject, not globally, so it's shorter than tokenBytes.
const idBytes = 8

// Token is one of a subject's active credentials.
type Token struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

// TokenInfo is Token without its Hash — what List returns for the
// panel's "Hesabım" page. The hash never needs to leave this package.
type TokenInfo struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store persists, per subject, the list of their active tokens. Unlike
// internal/access, every caller here already has a concrete Store (see
// cmd/devplatform/main.go) — there is no "optionally inert nil Store"
// case to support.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// now is a seam so tests could freeze CreatedAt if ever needed —
// matches the pattern internal/deploy/versionstore.go already
// established for the same reason.
var now = time.Now

// Generate creates a new random token for subject, labeled label, and
// ADDS it to subject's active tokens — it never invalidates any
// existing token (unlike the single-token model this replaced). This is
// the only moment the raw value exists outside the caller's memory; it
// is never stored and cannot be recovered afterward.
func (s *Store) Generate(subject, label string) (id, rawToken string, err error) {
	if subject == "" {
		return "", "", ErrInvalidSubject
	}

	rawID := make([]byte, idBytes)
	if _, err := rand.Read(rawID); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(rawID)

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	rawToken = base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return "", "", err
	}
	registry[subject] = append(registry[subject], Token{
		ID:        id,
		Hash:      hash(rawToken),
		Label:     label,
		CreatedAt: now().UTC(),
	})
	if err := s.save(registry); err != nil {
		return "", "", err
	}
	return id, rawToken, nil
}

// List returns subject's active tokens, newest first, without their
// hashes.
func (s *Store) List(subject string) ([]TokenInfo, error) {
	if subject == "" {
		return nil, ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return nil, err
	}
	tokens := registry[subject]
	out := make([]TokenInfo, len(tokens))
	for i, t := range tokens {
		// Generate appends, so the stored order is oldest-first — reverse
		// it so List reads newest-first, the order the panel wants.
		out[len(tokens)-1-i] = TokenInfo{ID: t.ID, Label: t.Label, CreatedAt: t.CreatedAt}
	}
	return out, nil
}

// Revoke removes subject's token with the given id, if any. A missing
// id is not an error — idempotent, the same convention
// internal/access.Store.Clear uses.
func (s *Store) Revoke(subject, id string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	tokens := registry[subject]
	out := tokens[:0]
	for _, t := range tokens {
		if t.ID != id {
			out = append(out, t)
		}
	}
	registry[subject] = out
	return s.save(registry)
}

// RevokeAll removes every one of subject's active tokens — "cut off
// this person's git access entirely," what the admin-only
// DELETE /api/git-token/{subject} route performs.
func (s *Store) RevokeAll(subject string) error {
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

// Verify reports whether token is one of subject's active tokens. Load
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

	got := hash(token)
	matched := false
	for _, t := range registry[subject] {
		// Compare against every stored hash, not just until the first
		// match — stopping early would make timing depend on which
		// token (if any) matched.
		if subtle.ConstantTimeCompare([]byte(got), []byte(t.Hash)) == 1 {
			matched = true
		}
	}
	return matched
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// load reads the registry. A missing file is an empty registry, not an
// error — nobody has generated a token yet.
func (s *Store) load() (map[string][]Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Token{}, nil
		}
		return nil, err
	}
	registry := map[string][]Token{}
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
// internal/access.Store.save uses.
func (s *Store) save(registry map[string][]Token) error {
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
Expected: PASS — all tests green (the handlers_test.go in this same
package will still fail to compile at this point — that's Task 2, not a
regression from this step).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gittoken/store.go backend/internal/gittoken/store_test.go
git commit -m "feat(gittoken): support multiple independently-revocable tokens per subject"
```

---

### Task 2: `gittoken.Handlers` + server wiring

**Files:**
- Modify: `backend/internal/gittoken/handlers.go`
- Modify: `backend/internal/gittoken/handlers_test.go` (full replace)
- Modify: `backend/internal/server/server.go`

**Interfaces:**
- Consumes: `Store.Generate(subject, label)`, `Store.List(subject)`,
  `Store.Revoke(subject, id)`, `Store.RevokeAll(subject)` (Task 1).
- Produces: `Handlers.GenerateMine` (response shape now
  `{"id": "...", "token": "..."}`, request body now
  `{"label": "..."}`), `Handlers.ListMine` (new), `Handlers.RevokeMine`
  (new), `Handlers.Revoke` (unchanged signature, now calls `RevokeAll`).

- [ ] **Step 1: Replace the test file**

Replace the entire contents of `backend/internal/gittoken/handlers_test.go`:

```go
package gittoken

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func authMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
}

func TestHandlers_GenerateMine_ReturnsATokenForTheAuthenticatedCaller(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	mux := http.NewServeMux()
	mux.Handle("POST /api/me/git-token", authMiddleware()(http.HandlerFunc(h.GenerateMine)))

	req := httptest.NewRequest(http.MethodPost, "/api/me/git-token", strings.NewReader(`{"label":"laptop"}`))
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Token == "" || body.ID == "" {
		t.Fatalf("response missing id or token: %+v", body)
	}
	if !h.Store.Verify("dev-1", body.Token) {
		t.Error("returned token does not verify against the store")
	}
}

func TestHandlers_GenerateMine_RejectsUnauthenticatedRequests(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	mux := http.NewServeMux()
	mux.Handle("POST /api/me/git-token", authMiddleware()(http.HandlerFunc(h.GenerateMine)))

	req := httptest.NewRequest(http.MethodPost, "/api/me/git-token", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlers_ListMine_ReturnsOnlyTheCallersTokens(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	if _, _, err := h.Store.Generate("dev-1", "laptop"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if _, _, err := h.Store.Generate("dev-2", "someone else's"); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/me/git-tokens", authMiddleware()(http.HandlerFunc(h.ListMine)))

	req := httptest.NewRequest(http.MethodGet, "/api/me/git-tokens", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var tokens []TokenInfo
	if err := json.NewDecoder(rec.Body).Decode(&tokens); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Label != "laptop" {
		t.Errorf("tokens = %+v, want exactly dev-1's one token", tokens)
	}
}

func TestHandlers_RevokeMine_OnlyRevokesTheCallersOwnToken(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	id, token, err := h.Store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("DELETE /api/me/git-tokens/{id}", authMiddleware()(http.HandlerFunc(h.RevokeMine)))

	req := httptest.NewRequest(http.MethodDelete, "/api/me/git-tokens/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if h.Store.Verify("dev-1", token) {
		t.Error("token still verifies after RevokeMine")
	}
}

func TestHandlers_Revoke_InvalidatesEveryTokenForTheSubject(t *testing.T) {
	h := &Handlers{Store: NewStore(t.TempDir() + "/git-tokens.json")}
	_, token1, err := h.Store.Generate("dev-1", "laptop")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	_, token2, err := h.Store.Generate("dev-1", "desktop")
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
	if h.Store.Verify("dev-1", token1) || h.Store.Verify("dev-1", token2) {
		t.Error("Revoke (admin) left at least one token still valid")
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
Expected: FAIL — `h.Store.Generate` called with 2 args (compile error
against Task 1's new signature is already fine, but `ListMine`/
`RevokeMine` are undefined on `Handlers`).

- [ ] **Step 3: Replace the implementation**

Replace the entire contents of `backend/internal/gittoken/handlers.go`:

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

type generateRequest struct {
	Label string `json:"label"`
}

// GenerateMine handles POST /api/me/git-token, meant to be mounted
// behind auth.RequireAuth only (no role requirement — anyone can mint
// their own key). It always acts on the caller's own JWT subject; there
// is deliberately no path parameter, so nobody can request a token on
// someone else's behalf through this endpoint. An empty or missing
// "label" is accepted, not an error — the panel and the CLI login tool
// always supply one, but this shouldn't be the reason a bare
// `curl -X POST` fails.
func (h *Handlers) GenerateMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	var req generateRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	id, token, err := h.Store.Generate(user.Subject, req.Label)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "token": token})
}

// ListMine handles GET /api/me/git-tokens — returns the caller's own
// active tokens (id, label, createdAt — never a hash).
func (h *Handlers) ListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	tokens, err := h.Store.List(user.Subject)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tokens)
}

// RevokeMine handles DELETE /api/me/git-tokens/{id} — revokes one of
// the caller's own tokens. Always acts on the caller's own subject
// (from the JWT, not a path parameter), so nobody can revoke someone
// else's token through this endpoint — that's what the admin-only
// Revoke below is for.
func (h *Handlers) RevokeMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "400 id is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Revoke(user.Subject, id); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Revoke handles DELETE /api/git-token/{subject}, meant to be mounted
// behind auth.RequireRole(auth.RoleAdmin, ...) — revokes EVERY one of
// subject's tokens (see Store.RevokeAll), the "cut off this person's
// git access entirely" admin action, mirroring
// internal/access.Handlers.Clear's admin-only pattern for
// /api/access/{subject}.
func (h *Handlers) Revoke(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.RevokeAll(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/gittoken/... -v`
Expected: PASS — all tests green.

- [ ] **Step 5: Add the two new routes to `server.go`**

In `backend/internal/server/server.go`, find the existing lines:
```go
	mux.Handle("POST /api/me/git-token", authMiddleware(http.HandlerFunc(gitTokens.GenerateMine)))
	mux.Handle("DELETE /api/git-token/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(gitTokens.Revoke))))
```
Leave those two exactly as they are (their handler bodies changed in
Step 3, but the route registrations themselves don't need to change),
and add two new lines directly after them:
```go
	mux.Handle("GET /api/me/git-tokens", authMiddleware(http.HandlerFunc(gitTokens.ListMine)))
	mux.Handle("DELETE /api/me/git-tokens/{id}", authMiddleware(http.HandlerFunc(gitTokens.RevokeMine)))
```

- [ ] **Step 6: Run the full backend test suite**

Run: `cd backend && go test ./...`
Expected: PASS, no regressions elsewhere.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/gittoken/handlers.go backend/internal/gittoken/handlers_test.go backend/internal/server/server.go
git commit -m "feat(gittoken): expose list/revoke-mine endpoints, admin revoke now revokes all"
```

---

### Task 3: Frontend — Hesabım page shows a list of tokens

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/pages/HesabimPage.tsx` (full replace)

**Interfaces:**
- Consumes: `GET /api/me/git-tokens`, `POST /api/me/git-token` (now
  takes `{label}`, returns `{id, token}`),
  `DELETE /api/me/git-tokens/{id}` (Task 2).
- Produces: `GitTokenInfo` type; `api.listGitTokens()`,
  `api.generateGitToken(label)`, `api.revokeMyGitToken(id)`.

- [ ] **Step 1: Add the type**

In `frontend/src/api/types.ts`, near `AccessRegistry`, add:
```ts
// One of a person's active git credentials (see
// backend/internal/gittoken). A person can have several at once — one
// per machine/CLI login is the expected pattern — each independently
// revocable; generating a new one never invalidates an existing one.
export interface GitTokenInfo {
  id: string
  label: string
  createdAt: string
}
```

- [ ] **Step 2: Update the API client**

In `frontend/src/api/client.ts`, add `GitTokenInfo` to the type-only
import list at the top.

Find:
```ts
  generateGitToken: () => request<{ token: string }>('/api/me/git-token', { method: 'POST' }),
  revokeGitToken: (subject: string) =>
    request<void>(`/api/git-token/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
```
Replace with:
```ts
  generateGitToken: (label: string) =>
    request<{ id: string; token: string }>('/api/me/git-token', {
      method: 'POST',
      body: JSON.stringify({ label }),
    }),
  listGitTokens: () => request<GitTokenInfo[]>('/api/me/git-tokens'),
  revokeMyGitToken: (id: string) =>
    request<void>(`/api/me/git-tokens/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  // Admin-only — revokes EVERY one of subject's tokens (see
  // backend/internal/gittoken.Store.RevokeAll). Unchanged by this
  // feature; still used from AccessPage.tsx's "Git anahtarını iptal et".
  revokeGitToken: (subject: string) =>
    request<void>(`/api/git-token/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
```

- [ ] **Step 3: Replace `HesabimPage.tsx`**

Replace the entire contents of `frontend/src/pages/HesabimPage.tsx`:

```tsx
import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type GitTokenInfo } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { formatDate } from '../labels'

// HesabimPage lets the signed-in person manage their own git
// credentials — see
// docs/superpowers/specs/2026-09-03-cli-git-login-design.md. A person
// can have several active tokens at once (one per machine/CLI login),
// each independently revocable — generating a new one never
// invalidates an existing one (see backend/internal/gittoken). The raw
// value of a newly generated token is shown exactly once, right after
// generation; it is never stored or re-displayed — only its SHA-256
// hash persists on the server.
export function HesabimPage() {
  const { user } = useAuth()
  const [tokens, setTokens] = useState<GitTokenInfo[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [label, setLabel] = useState('')
  const [newToken, setNewToken] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function reload() {
    api
      .listGitTokens()
      .then(setTokens)
      .catch((err) => setListError(err instanceof ApiError ? err.message : 'Anahtarlar yüklenemedi'))
  }

  useEffect(reload, [])

  async function generate(e: FormEvent) {
    e.preventDefault()
    if (!label.trim()) return
    setGenerating(true)
    setGenError(null)
    setCopied(false)
    try {
      const res = await api.generateGitToken(label.trim())
      setNewToken(res.token)
      setLabel('')
      reload()
    } catch (err) {
      setGenError(err instanceof ApiError ? err.message : 'Anahtar oluşturulamadı')
    } finally {
      setGenerating(false)
    }
  }

  async function revoke(id: string) {
    if (
      !confirm(
        'Bu anahtarı iptal etmek istediğinize emin misiniz? Bu anahtarı kullanan makine/araç artık git işlemi yapamaz.',
      )
    ) {
      return
    }
    try {
      await api.revokeMyGitToken(id)
      reload()
    } catch (err) {
      setListError(err instanceof ApiError ? err.message : 'İptal edilemedi')
    }
  }

  async function copy() {
    if (!newToken) return
    try {
      await navigator.clipboard.writeText(newToken)
      setCopied(true)
    } catch {
      setGenError('Panoya kopyalanamadı — anahtarı aşağıdaki kutudan seçip elle kopyalayın.')
    }
  }

  const subject = user?.subject ?? ''
  const cloneExample = `git clone http://${encodeURIComponent(subject)}:${newToken ?? '<anahtar>'}@${window.location.host}/git/<repo>.git`

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Hesabım</h1>
          <p className="page-subtitle">Git üzerinden clone/push yapmak için kişisel anahtarlarınız</p>
        </div>
      </div>

      <div className="card">
        <p>
          Git'e paylaşılan bir şifreyle değil, kendinize ait anahtarlarla bağlanırsınız. Panelde hangi
          repolara erişebiliyorsanız git'te de aynı repolara erişebilirsiniz — ayrı bir izin sistemi
          yok. Birden fazla anahtarınız olabilir (örn. her makine için ayrı) — yeni bir anahtar
          oluşturmak diğerlerini geçersiz kılmaz.
        </p>

        {listError && <p className="error">{listError}</p>}
        {tokens === null && !listError && <p className="empty-state">Yükleniyor...</p>}
        {tokens?.length === 0 && <p className="empty-state">Henüz aktif bir anahtarınız yok.</p>}
        {tokens && tokens.length > 0 && (
          <ul className="row-list">
            {tokens.map((t) => (
              <li key={t.id}>
                <div className="row-main">
                  <span className="row-title">{t.label || '(etiketsiz)'}</span>
                  <span className="spacer" />
                  <span className="muted">{formatDate(t.createdAt)}</span>
                  <button type="button" className="btn-ghost" onClick={() => revoke(t.id)}>
                    İptal et
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="section-title">
        <h2>Yeni anahtar oluştur</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <form onSubmit={generate} className="stacked-form">
            <div className="field">
              <label htmlFor="token-label">Etiket</label>
              <input
                id="token-label"
                type="text"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="örn. iş dizüstü bilgisayarım"
              />
            </div>
            <div className="form-actions">
              <button type="submit" className="btn-primary" disabled={generating || !label.trim()}>
                {generating ? 'Oluşturuluyor...' : 'Anahtar oluştur'}
              </button>
            </div>
            {genError && <p className="error">{genError}</p>}
          </form>

          {newToken && (
            <div className="field">
              <label>Yeni anahtarınız</label>
              <p className="error">Bu anahtar bir daha gösterilmeyecek — şimdi bir yere kaydedin.</p>
              <textarea
                readOnly
                rows={2}
                value={newToken}
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
    </div>
  )
}
```

- [ ] **Step 4: Type-check**

Run: `cd frontend && npm run build`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts frontend/src/pages/HesabimPage.tsx
git commit -m "feat(frontend): Hesabım shows a list of git tokens instead of one"
```

---

### Task 4: `devplatform-login` — DPAPI-encrypted local cache

**Files:**
- Create: `backend/cmd/devplatform-login/cache.go`
- Test: `backend/cmd/devplatform-login/cache_test.go`
- Create: `backend/go.mod`, `backend/go.sum` (modify — new dependency)

**Interfaces:**
- Produces: `cachedCredential{Subject, Token, CachedAt}`,
  `loadCache() (*cachedCredential, error)`,
  `saveCache(cred cachedCredential) error`, `clearCache() error`,
  `dpapiProtect([]byte) ([]byte, error)`,
  `dpapiUnprotect([]byte) ([]byte, error)`.

This task's DPAPI wrapper code was written and verified compiling +
round-tripping on this machine during design — Step 3's code is exactly
that verified code, not a first attempt.

- [ ] **Step 1: Write the failing tests**

Create `backend/cmd/devplatform-login/cache_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestDPAPI_ProtectThenUnprotect_RoundTrips(t *testing.T) {
	plaintext := []byte(`{"subject":"dev-1","token":"abc123"}`)

	encrypted, err := dpapiProtect(plaintext)
	if err != nil {
		t.Fatalf("dpapiProtect returned error: %v", err)
	}
	if string(encrypted) == string(plaintext) {
		t.Fatal("dpapiProtect returned the plaintext unchanged")
	}

	decrypted, err := dpapiUnprotect(encrypted)
	if err != nil {
		t.Fatalf("dpapiUnprotect returned error: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip = %q, want %q", decrypted, plaintext)
	}
}

func TestSaveCache_ThenLoadCache_RoundTrips(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	want := cachedCredential{Subject: "dev-1", Token: "abc123", CachedAt: time.Now().UTC().Truncate(time.Second)}
	if err := saveCache(want); err != nil {
		t.Fatalf("saveCache returned error: %v", err)
	}

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got == nil {
		t.Fatal("loadCache returned nil after saveCache")
	}
	if got.Subject != want.Subject || got.Token != want.Token {
		t.Errorf("loadCache = %+v, want %+v", got, want)
	}
}

func TestLoadCache_MissingFileReturnsNilNotError(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got != nil {
		t.Errorf("loadCache = %+v, want nil (no cache file yet)", got)
	}
}

func TestClearCache_ThenLoadCache_ReturnsNil(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := saveCache(cachedCredential{Subject: "dev-1", Token: "abc123", CachedAt: time.Now()}); err != nil {
		t.Fatalf("saveCache returned error: %v", err)
	}

	if err := clearCache(); err != nil {
		t.Fatalf("clearCache returned error: %v", err)
	}

	got, err := loadCache()
	if err != nil {
		t.Fatalf("loadCache returned error: %v", err)
	}
	if got != nil {
		t.Errorf("loadCache after clearCache = %+v, want nil", got)
	}
}

func TestClearCache_NoCacheFileIsNotAnError(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	if err := clearCache(); err != nil {
		t.Errorf("clearCache with no cache file returned error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./cmd/devplatform-login/... -v`
Expected: FAIL — the package/functions don't exist yet (this is the
first file in a brand-new `cmd/devplatform-login` directory).

- [ ] **Step 3: Write the implementation**

Create `backend/cmd/devplatform-login/cache.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// cachedCredential is what's persisted (DPAPI-encrypted) between runs.
type cachedCredential struct {
	Subject  string    `json:"subject"`
	Token    string    `json:"token"`
	CachedAt time.Time `json:"cachedAt"`
}

func cachePath() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "devplatform", "credential"), nil
}

// loadCache returns the cached credential, or (nil, nil) if there is
// none yet (missing file — the normal first-run state, not an error).
func loadCache() (*cachedCredential, error) {
	path, err := cachePath()
	if err != nil {
		return nil, err
	}
	encrypted, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	plaintext, err := dpapiUnprotect(encrypted)
	if err != nil {
		// A cache file that no longer decrypts (copied from another
		// machine/user, or corrupted) is treated the same as "no cache" —
		// the next login attempt just creates a fresh one, rather than
		// this tool refusing to work at all.
		return nil, nil
	}
	var cred cachedCredential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		return nil, nil
	}
	return &cred, nil
}

// saveCache encrypts and persists cred, replacing any previous cache.
func saveCache(cred cachedCredential) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	plaintext, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	encrypted, err := dpapiProtect(plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0o600)
}

// clearCache removes the cached credential, if any — called on
// `erase` (git told us the credential it tried failed), so the next
// `get` starts a fresh login instead of handing out the same bad
// token again.
func clearCache() error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func dpapiProtect(plaintext []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(plaintext))}
	if len(plaintext) > 0 {
		in.Data = &plaintext[0]
	}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

func dpapiUnprotect(ciphertext []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(ciphertext))}
	if len(ciphertext) > 0 {
		in.Data = &ciphertext[0]
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobToBytes(out), nil
}

func blobToBytes(b windows.DataBlob) []byte {
	result := unsafe.Slice(b.Data, b.Size)
	cp := make([]byte, len(result))
	copy(cp, result)
	return cp
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./cmd/devplatform-login/... -v`
Expected: PASS — all 5 tests green. (`golang.org/x/sys/windows` is
already a `backend/go.mod` dependency — no `go get` needed for this
task.)

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/devplatform-login/cache.go backend/cmd/devplatform-login/cache_test.go
git commit -m "feat(devplatform-login): add DPAPI-encrypted local credential cache"
```

---

### Task 5: `devplatform-login` — the 3-step login chain

**Files:**
- Create: `backend/cmd/devplatform-login/login.go`
- Test: `backend/cmd/devplatform-login/login_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (this file is self-contained —
  it's combined with Task 4's cache in Task 6).
- Produces: `login(username, password string) (subject, token string, err error)`,
  package-level `var intranetBaseURL`, `var devplatformBaseURL` (test
  seams).

- [ ] **Step 1: Write the failing tests**

Create `backend/cmd/devplatform-login/login_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func signTestJWT(t *testing.T, subject string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": subject})
	s, err := tok.SignedString([]byte("irrelevant-since-unverified"))
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	return s
}

func TestLogin_ChainsAllThreeCallsAndReturnsTheGitToken(t *testing.T) {
	devplatformJWT := signTestJWT(t, "dev-1")

	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["Username"] != "rifat" || body["Password"] != "sifre123" {
				t.Errorf("login body = %v, want rifat/sifre123", body)
			}
			json.NewEncoder(w).Encode(map[string]string{"token": "intranet-jwt"})
		case "/api/auth/devplatform-sso":
			if got := r.Header.Get("Authorization"); got != "Bearer intranet-jwt" {
				t.Errorf("devplatform-sso Authorization = %q, want Bearer intranet-jwt", got)
			}
			json.NewEncoder(w).Encode(map[string]string{"token": devplatformJWT})
		default:
			t.Errorf("unexpected intranet request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer intranet.Close()

	devplatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/git-token" {
			t.Errorf("unexpected devplatform request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+devplatformJWT {
			t.Errorf("git-token Authorization = %q, want Bearer %s", got, devplatformJWT)
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "abc", "token": "final-git-token"})
	}))
	defer devplatform.Close()

	origIntranet, origDevplatform := intranetBaseURL, devplatformBaseURL
	intranetBaseURL, devplatformBaseURL = intranet.URL, devplatform.URL
	defer func() { intranetBaseURL, devplatformBaseURL = origIntranet, origDevplatform }()

	subject, token, err := login("rifat", "sifre123")
	if err != nil {
		t.Fatalf("login returned error: %v", err)
	}
	if subject != "dev-1" {
		t.Errorf("subject = %q, want %q", subject, "dev-1")
	}
	if token != "final-git-token" {
		t.Errorf("token = %q, want %q", token, "final-git-token")
	}
}

func TestLogin_ReturnsAClearErrorOn403FromDevplatformSSO(t *testing.T) {
	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "intranet-jwt"})
		case "/api/auth/devplatform-sso":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer intranet.Close()

	origIntranet := intranetBaseURL
	intranetBaseURL = intranet.URL
	defer func() { intranetBaseURL = origIntranet }()

	_, _, err := login("rifat", "yanlis-sifre")
	if err == nil {
		t.Fatal("login returned no error for a 403 from devplatform-sso")
	}
}

func TestLogin_ReturnsAClearErrorOnBadIntranetCredentials(t *testing.T) {
	intranet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer intranet.Close()

	origIntranet := intranetBaseURL
	intranetBaseURL = intranet.URL
	defer func() { intranetBaseURL = origIntranet }()

	_, _, err := login("rifat", "yanlis-sifre")
	if err == nil {
		t.Fatal("login returned no error for a 401 from intranet login")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./cmd/devplatform-login/... -run TestLogin -v`
Expected: FAIL — `login`, `intranetBaseURL`, `devplatformBaseURL`
undefined.

- [ ] **Step 3: Write the implementation**

Create `backend/cmd/devplatform-login/login.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// intranetBaseURL and devplatformBaseURL are vars, not consts, so
// tests can point them at an httptest.Server — the same seam pattern
// internal/deploy/versionstore.go uses for time.Now.
var (
	intranetBaseURL    = "https://intranet.sigortatahkim.org"
	devplatformBaseURL = "https://git.sigortatahkim.org"
)

// login runs the 3-step exchange (Intranet-B login -> devplatform-sso
// -> DevPlatform git-token) and returns the resulting subject and git
// token. The AD password is only ever held in this process's memory —
// never written to disk.
func login(username, password string) (subject, token string, err error) {
	intranetJWT, err := intranetLogin(username, password)
	if err != nil {
		return "", "", fmt.Errorf("intranet girişi başarısız: %w", err)
	}

	devplatformJWT, err := devplatformSSO(intranetJWT)
	if err != nil {
		return "", "", fmt.Errorf("devplatform yetkisi alınamadı: %w", err)
	}

	_, gitToken, err := mintGitToken(devplatformJWT, hostLabel())
	if err != nil {
		return "", "", fmt.Errorf("git anahtarı alınamadı: %w", err)
	}

	subject, err = jwtSubject(devplatformJWT)
	if err != nil {
		return "", "", fmt.Errorf("devplatform token'ı okunamadı: %w", err)
	}
	return subject, gitToken, nil
}

func intranetLogin(username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	resp, err := http.Post(intranetBaseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("intranet girişi %d döndü — kullanıcı adı/şifre hatalı olabilir", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("intranet girişi bir token döndürmedi")
	}
	return parsed.Token, nil
}

func devplatformSSO(intranetJWT string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, intranetBaseURL+"/api/auth/devplatform-sso", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+intranetJWT)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("bu hesaba DevPlatform yetkisi verilmemiş (403) — admin panelinden yetki verilmesi gerekiyor")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("devplatform-sso %d döndü", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("devplatform-sso bir token döndürmedi")
	}
	return parsed.Token, nil
}

func mintGitToken(devplatformJWT, label string) (id, token string, err error) {
	body, _ := json.Marshal(map[string]string{"label": label})
	req, err := http.NewRequest(http.MethodPost, devplatformBaseURL+"/api/me/git-token", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+devplatformJWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("git-token %d döndü: %s", resp.StatusCode, respBody)
	}
	var parsed struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", err
	}
	return parsed.ID, parsed.Token, nil
}

// jwtSubject reads the "sub" claim out of a JWT without verifying its
// signature — safe here because this JWT was just received directly
// from DevPlatform itself over HTTPS a moment ago, not supplied by an
// untrusted caller.
func jwtSubject(tokenString string) (string, error) {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(tokenString, claims); err != nil {
		return "", err
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", fmt.Errorf("token has no sub claim")
	}
	return sub, nil
}

// hostLabel names the token after this machine, so the "Hesabım" list
// shows which device each active token belongs to. Falls back to a
// generic label if the hostname can't be read — that's a cosmetic
// detail, not worth failing the whole login over.
func hostLabel() string {
	name, err := os.Hostname()
	if err != nil {
		return "CLI"
	}
	return "CLI - " + name
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./cmd/devplatform-login/... -v`
Expected: PASS — all tests from Task 4 and Task 5 green.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/devplatform-login/login.go backend/cmd/devplatform-login/login_test.go
git commit -m "feat(devplatform-login): add the Intranet-B -> DevPlatform login chain"
```

---

### Task 6: `devplatform-login` — credential-helper protocol + `install`

**Files:**
- Create: `backend/cmd/devplatform-login/main.go`
- Create: `backend/cmd/devplatform-login/console.go`
- Test: `backend/cmd/devplatform-login/main_test.go`
- Modify: `backend/go.mod`, `backend/go.sum` (new dependency:
  `golang.org/x/term` — verified compiling on this machine during
  design)

**Interfaces:**
- Consumes: `loadCache`, `saveCache`, `clearCache`, `cachedCredential`
  (Task 4); `login` (Task 5).
- Produces: the `devplatform-login` binary's `get`/`store`/`erase`/
  `install` command-line behavior — nothing further consumes this in
  code, it's the plan's final deliverable.

- [ ] **Step 1: Add the new dependency**

Run: `cd backend && go get golang.org/x/term`
Expected: `go.mod`/`go.sum` gain a `golang.org/x/term` entry. This
package was verified compiling (`term.ReadPassword(int(fd)) ([]byte, error)`)
on this machine during design — Step 4's code uses that exact,
already-confirmed API shape.

- [ ] **Step 2: Write the failing test**

Create `backend/cmd/devplatform-login/main_test.go`:

```go
package main

import "testing"

func TestInstallCommand_UsesTheExactHelperConfigKey(t *testing.T) {
	cmd := installCommand(`C:\tools\devplatform-login.exe`)

	want := []string{
		"git", "config", "--global",
		"credential.https://git.sigortatahkim.org.helper", `C:\tools\devplatform-login.exe`,
	}
	got := cmd.Args
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./cmd/devplatform-login/... -run TestInstallCommand -v`
Expected: FAIL — `installCommand` undefined.

- [ ] **Step 4: Write `console.go`**

Create `backend/cmd/devplatform-login/console.go`:

```go
package main

import "os"

// openConsole opens the real console device directly, bypassing
// whatever stdin/stdout have been redirected to. Git owns stdin/stdout
// during `get`/`store`/`erase` for its own credential protocol, so an
// interactive prompt can't use them — "CONIN$"/"CONOUT$" are Windows'
// special device names for the calling process's actual console
// regardless of redirection, the standard mechanism interactive
// command-line credential helpers use for exactly this situation.
func openConsole() (in, out *os.File, err error) {
	in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}
```

- [ ] **Step 5: Write `main.go`**

Create `backend/cmd/devplatform-login/main.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: devplatform-login <get|store|erase|install>")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "get":
		runGet()
	case "store":
		runStore()
	case "erase":
		runErase()
	case "install":
		runInstall()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

// drainProtocolInput reads and discards git's credential-protocol
// input on stdin (key=value lines, terminated by EOF) — this tool is
// only ever configured for one host (git.sigortatahkim.org, via
// `install`'s git-config), so there is nothing in the input worth
// inspecting; it still has to be read so git's pipe to us doesn't
// block.
func drainProtocolInput() {
	io.Copy(io.Discard, os.Stdin)
}

func runGet() {
	drainProtocolInput()

	cred, err := loadCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: önbellek okunamadı: %v\n", err)
		os.Exit(1)
	}
	if cred != nil {
		fmt.Printf("username=%s\npassword=%s\n", cred.Subject, cred.Token)
		return
	}

	subject, token, err := promptAndLogin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: giriş başarısız: %v\n", err)
		os.Exit(1)
	}
	if err := saveCache(cachedCredential{Subject: subject, Token: token, CachedAt: time.Now()}); err != nil {
		// A cache write failure shouldn't block this login from working
		// right now — it just means the next git operation prompts again.
		fmt.Fprintf(os.Stderr, "devplatform-login: uyarı: token önbelleğe yazılamadı: %v\n", err)
	}
	fmt.Printf("username=%s\npassword=%s\n", subject, token)
}

func runStore() {
	drainProtocolInput()
	// No-op: this tool populates its own cache during `get`, it doesn't
	// need git to confirm a credential worked.
}

func runErase() {
	drainProtocolInput()
	if err := clearCache(); err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: önbellek temizlenemedi: %v\n", err)
		os.Exit(1)
	}
}

func installCommand(selfPath string) *exec.Cmd {
	return exec.Command("git", "config", "--global",
		"credential.https://git.sigortatahkim.org.helper", selfPath)
}

func runInstall() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: kendi yolum bulunamadı: %v\n", err)
		os.Exit(1)
	}
	cmd := installCommand(self)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "devplatform-login: git config başarısız: %v\n%s\n", err, out)
		os.Exit(1)
	}
	fmt.Println("Kuruldu. Artık git.sigortatahkim.org için git işlemleri otomatik kimlik doğrulayacak.")
}

// promptAndLogin opens the real console directly (not stdin/stdout,
// which `get` has already reserved for git's own protocol) to ask for
// credentials interactively, then runs the login chain.
func promptAndLogin() (subject, token string, err error) {
	in, out, err := openConsole()
	if err != nil {
		return "", "", fmt.Errorf("konsol açılamadı (bu araç etkileşimli bir terminalden çalıştırılmalı): %w", err)
	}
	defer in.Close()
	defer out.Close()

	fmt.Fprint(out, "STK Atölye kullanıcı adı: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", "", fmt.Errorf("kullanıcı adı okunamadı")
	}
	username := scanner.Text()

	fmt.Fprint(out, "Şifre: ")
	passwordBytes, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", "", fmt.Errorf("şifre okunamadı: %w", err)
	}

	return login(username, string(passwordBytes))
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd backend && go test ./cmd/devplatform-login/... -v`
Expected: PASS — all tests across Tasks 4, 5, and 6 green.

- [ ] **Step 7: Build the binary**

Run: `cd backend && go build -o ../dist/devplatform-login.exe ./cmd/devplatform-login`
Expected: builds clean, produces `dist/devplatform-login.exe`.

- [ ] **Step 8: Run the full backend test suite**

Run: `cd backend && go test ./...`
Expected: PASS, no regressions anywhere (in particular
`internal/gittoken`, `internal/server`).

- [ ] **Step 9: Commit**

```bash
git add backend/cmd/devplatform-login/main.go backend/cmd/devplatform-login/console.go backend/cmd/devplatform-login/main_test.go backend/go.mod backend/go.sum
git commit -m "feat(devplatform-login): implement get/store/erase and install"
```

- [ ] **Step 10: Flag what needs a live, manual verification pass**

This cannot be exercised by `go test` — it needs a real terminal, a
real AD account, and both `devplatform-login.exe` and the server-side
changes (Tasks 1-2) actually deployed. Note in the final report to the
human partner that the following still needs a supervised live check,
matching this codebase's own established pattern for IIS/console-
dependent behavior (see `docs/DURUM.md`'s "gözetimli doğrulama" notes):

1. `devplatform-login.exe install` actually sets the git config key.
2. A fresh `git clone https://git.sigortatahkim.org/git/<repo>.git`
   with no cached credential prompts for username/password in the
   console (not swallowed by a redirected stdin), and succeeds after a
   correct login.
3. A second `git pull` right after does **not** prompt again (cache
   hit).
4. Deleting/corrupting the cache file, or having an admin revoke the
   token from the panel, makes the *next* git operation prompt again
   automatically (via `erase`) rather than failing with an opaque 401.
5. A wrong password shows a clear Turkish error message, not a raw Go
   error or an AD-internal message.
