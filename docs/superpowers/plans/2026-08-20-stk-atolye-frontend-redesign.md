# STK Atölye Frontend Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename DevPlatform's panel to "STK Atölye", add a subject→display-name override so people are shown by name instead of email, split repo-scoped navigation out of the global sidebar into a GitHub-style top tab bar, and turn the task list into a 3-column Jira-style kanban board.

**Architecture:** Two small, independent backend additions (a `displaynames` package mirroring the existing `access` package's file-store pattern, and one new field on `/api/me`'s response), followed by frontend-only changes: a new `RepoTabBar` component, an "Görünen adlar" admin section reusing `AccessPage`'s existing people list, and a full rewrite of `RepoTasksPage` from a flat list to a drag-and-drop kanban board.

**Tech Stack:** Go 1.21+ backend (stdlib `net/http`, existing `internal/access`-style JSON file store), React 19 + TypeScript frontend (native HTML5 drag-and-drop, no new dependency), Vite build.

**Spec:** `docs/superpowers/specs/2026-08-20-stk-atolye-frontend-redesign-design.md`

## Global Constraints

- No new `TaskStatus` value is introduced — the kanban board has exactly the
  three columns `in_progress` / `awaiting_test` / `done` that
  `backend/internal/taskboard/taskboard.go` already defines. `taskboard.Create`
  keeps starting every task at `StatusInProgress`; nothing here changes that.
- The existing color/token system (`frontend/src/index.css`'s `--accent`,
  `--success`, `--warn`, `--danger` and the `TASK_STATUS_BADGE` map in
  `frontend/src/labels.ts`) is reused as-is. No new color tokens.
- No new frontend npm dependency — the kanban board's drag-and-drop uses the
  native HTML5 Drag and Drop API (`draggable`, `onDragStart`, `onDragOver`,
  `onDrop`), not a library.
- The rename to "STK Atölye" is a **display-text-only** change: the npm
  package name (`frontend/package.json`'s `"name"`), the Go module path
  (`go.mod`), and any internal identifiers/env var prefixes
  (`DEVPLATFORM_*`) are explicitly **not** renamed — only text a user reads
  on screen changes.
- The logo mark (`components/icons.tsx`'s `LogoMark`) is unchanged.
- Repos/Audit/Notifications/Deploy Hedefleri pages get no structural or
  route changes in this plan.

---

### Task 1: Rename the panel to "STK Atölye"

**Files:**
- Modify: `frontend/index.html:7`
- Modify: `frontend/src/components/AppLayout.tsx:45`
- Modify: `frontend/src/pages/LoginPage.tsx:26`

**Interfaces:** None — pure text change, no new exports or types.

- [ ] **Step 1: Change the browser tab title**

In `frontend/index.html`, change line 7 from:
```html
    <title>DevPlatform</title>
```
to:
```html
    <title>STK Atölye</title>
```

- [ ] **Step 2: Change the top bar brand text**

In `frontend/src/components/AppLayout.tsx`, find:
```tsx
        <Link to="/" className="brand">
          <LogoMark className="brand-mark" />
          DevPlatform
        </Link>
```
and change the text node to:
```tsx
        <Link to="/" className="brand">
          <LogoMark className="brand-mark" />
          STK Atölye
        </Link>
```

- [ ] **Step 3: Change the login screen brand text**

In `frontend/src/pages/LoginPage.tsx`, find:
```tsx
        <div className="login-brand">
          <LogoMark className="brand-mark" />
          <span>DevPlatform</span>
        </div>
```
and change to:
```tsx
        <div className="login-brand">
          <LogoMark className="brand-mark" />
          <span>STK Atölye</span>
        </div>
```

- [ ] **Step 4: Build and visually confirm**

Run: `cd frontend && npm run build`
Expected: builds clean (no TypeScript errors — this task touches only JSX
text, so a failure here means a typo in the edit, not a real type issue).

Run `npm run dev`, open the app, confirm the tab title, top bar, and login
screen all read "STK Atölye" instead of "DevPlatform".

- [ ] **Step 5: Commit**

```bash
git add frontend/index.html frontend/src/components/AppLayout.tsx frontend/src/pages/LoginPage.tsx
git commit -m "feat(frontend): rename panel to STK Atölye"
```

---

### Task 2: Backend — `displaynames` package (Store)

**Files:**
- Create: `backend/internal/displaynames/displaynames.go`
- Test: `backend/internal/displaynames/displaynames_test.go`

**Interfaces:**
- Produces: `displaynames.NewStore(path string) *Store`, `(*Store).Get(subject, fallback string) string`, `(*Store).Set(subject, name string) error`, `(*Store).Clear(subject string) error`, `(*Store).List() (map[string]string, error)`, `displaynames.ErrInvalidSubject`.

This mirrors `backend/internal/access/access.go`'s file-store pattern
(atomic write via a temp file + rename, safe nil-receiver behavior, fresh
read from disk on every call) but stores a single string per subject
instead of a list of repos, and has no `RequireRepoAccess`-style
middleware — this package is display-only, not a security boundary.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/displaynames/displaynames_test.go`:

```go
package displaynames

import (
	"path/filepath"
	"testing"
)

func TestGet_ReturnsFallbackWhenNoOverrideIsSet(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestGet_NilStoreReturnsFallback(t *testing.T) {
	var store *Store

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get on nil store = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestSet_ThenGet_ReturnsTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "Rifat Öztürk" {
		t.Errorf("Get = %q, want %q", got, "Rifat Öztürk")
	}
}

func TestSet_RejectsEmptySubject(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Set("", "Rifat Öztürk"); err != ErrInvalidSubject {
		t.Errorf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestClear_RemovesTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if err := store.Clear("dev-1"); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}

	got := store.Get("dev-1", "dev-1@example.com")
	if got != "dev-1@example.com" {
		t.Errorf("Get after Clear = %q, want fallback %q", got, "dev-1@example.com")
	}
}

func TestClear_NonexistentSubjectIsNotAnError(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))

	if err := store.Clear("dev-1"); err != nil {
		t.Errorf("Clear on a subject with no override returned error: %v", err)
	}
}

func TestList_ReturnsEveryOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := store.Set("dev-2", "Ayşe Yılmaz"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	registry, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(registry) != 2 || registry["dev-1"] != "Rifat Öztürk" || registry["dev-2"] != "Ayşe Yılmaz" {
		t.Errorf("List = %v, want both overrides", registry)
	}
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "display-names.json")
	store1 := NewStore(path)
	if err := store1.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	store2 := NewStore(path)
	got := store2.Get("dev-1", "dev-1@example.com")
	if got != "Rifat Öztürk" {
		t.Errorf("a fresh Store instance backed by the same file: Get = %q, want %q", got, "Rifat Öztürk")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/displaynames/... -v`
Expected: FAIL — `package displaynames is not in std` / `undefined: NewStore` (the package doesn't exist yet).

- [ ] **Step 3: Write the implementation**

Create `backend/internal/displaynames/displaynames.go`:

```go
// Package displaynames lets an admin give a person a friendlier
// subject→"Ad Soyad" display name than whatever their SSO's JWT carries.
// The external identity system's JWT (see backend/internal/auth) only
// carries subject and email — no name claim — so this is the one place a
// human-readable name can be attached to a subject at all. Unlike
// internal/access, this is not a security boundary: an override here only
// changes what text is shown, never what a person can do or see, so there
// is no nil-means-locked-down posture to preserve — a nil Store or an
// unset subject simply means "show the fallback," exactly like an empty
// store would.
package displaynames

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var ErrInvalidSubject = errors.New("displaynames: subject must not be empty")

// Store persists, per subject, an optional display-name override in a
// single JSON file — same shape and atomic-write discipline as
// internal/access.Store, read fresh from disk on every call.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store backed by the file at path. The file does not
// need to exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Get returns subject's display-name override, or fallback (typically
// their email) if none is set. Safe to call on a nil Store — it behaves
// as "no overrides configured," always returning fallback.
func (s *Store) Get(subject, fallback string) string {
	if s == nil || subject == "" {
		return fallback
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return fallback
	}
	name, ok := registry[subject]
	if !ok || name == "" {
		return fallback
	}
	return name
}

// Set records subject's display-name override, replacing any previous one.
func (s *Store) Set(subject, name string) error {
	if subject == "" {
		return ErrInvalidSubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	registry, err := s.load()
	if err != nil {
		return err
	}
	registry[subject] = name
	return s.save(registry)
}

// Clear removes subject's override, if any. A subject with no override is
// not an error — matches internal/access.Store.Clear's idempotent-remove
// behavior.
func (s *Store) Clear(subject string) error {
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

// List returns every subject with an override, for the admin panel's
// management table. Safe to call on a nil Store — returns an empty map.
func (s *Store) List() (map[string]string, error) {
	if s == nil {
		return map[string]string{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

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

func (s *Store) save(registry map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}

	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".displaynames-*.tmp")
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

Run: `cd backend && go test ./internal/displaynames/... -v`
Expected: PASS — all 8 tests green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/displaynames
git commit -m "feat(displaynames): add subject-to-display-name Store"
```

---

### Task 3: Backend — HTTP handlers, `/api/me` field, and wiring

**Files:**
- Create: `backend/internal/displaynames/handlers.go`
- Test: `backend/internal/displaynames/handlers_test.go`
- Modify: `backend/internal/server/server.go`
- Test: `backend/internal/server/server_test.go`
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: `displaynames.NewStore`, `(*Store).List/Set/Clear/Get` from Task 2.
- Produces: `displaynames.Handlers{Store *Store}` with `List`, `Set`, `Clear` methods (mirrors `access.Handlers`'s shape exactly); `server.Deps.DisplayNames *displaynames.Store` field; `/api/me`'s JSON response gains a `displayName` field later frontend tasks read as `User.displayName`.

- [ ] **Step 1: Write the failing handler tests**

Create `backend/internal/displaynames/handlers_test.go`:

```go
package displaynames

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlers_List_ReturnsEveryOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodGet, "/api/display-names", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Rifat") {
		t.Errorf("body = %q, want it to contain the override", rec.Body.String())
	}
}

func TestHandlers_Set_StoresTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodPut, "/api/display-names/dev-1", strings.NewReader(`{"name":"Rifat Öztürk"}`))
	req.SetPathValue("subject", "dev-1")
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := store.Get("dev-1", "fallback"); got != "Rifat Öztürk" {
		t.Errorf("stored name = %q, want %q", got, "Rifat Öztürk")
	}
}

func TestHandlers_Set_RejectsMissingSubject(t *testing.T) {
	h := &Handlers{Store: NewStore(filepath.Join(t.TempDir(), "display-names.json"))}

	req := httptest.NewRequest(http.MethodPut, "/api/display-names/", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlers_Clear_RemovesTheOverride(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "display-names.json"))
	if err := store.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	h := &Handlers{Store: store}

	req := httptest.NewRequest(http.MethodDelete, "/api/display-names/dev-1", nil)
	req.SetPathValue("subject", "dev-1")
	rec := httptest.NewRecorder()
	h.Clear(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := store.Get("dev-1", "fallback"); got != "fallback" {
		t.Errorf("Get after Clear = %q, want fallback", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/displaynames/... -v -run TestHandlers`
Expected: FAIL — `undefined: Handlers`.

- [ ] **Step 3: Write the handlers implementation**

Create `backend/internal/displaynames/handlers.go`:

```go
package displaynames

import (
	"encoding/json"
	"net/http"
)

// Handlers exposes Store's admin operations as http.HandlerFuncs, meant to
// be mounted by internal/server entirely behind
// auth.RequireRole(auth.RoleAdmin, ...) — same posture as
// internal/access.Handlers, since only an admin should be renaming how
// someone else appears in the panel.
type Handlers struct {
	Store *Store
}

// List handles GET /api/display-names, returning every subject that has an
// override configured. A subject absent from the response falls back to
// their email in the panel — see Store's doc comment.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	registry, err := h.Store.List()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, registry)
}

type setRequest struct {
	Name string `json:"name"`
}

// Set handles PUT /api/display-names/{subject}, setting subject's
// display-name override to the request body's name.
func (h *Handlers) Set(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "400 malformed request body", http.StatusBadRequest)
		return
	}

	if err := h.Store.Set(subject, req.Name); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// Clear handles DELETE /api/display-names/{subject}, removing subject's
// override so the panel falls back to their email again.
func (h *Handlers) Clear(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	if subject == "" {
		http.Error(w, "400 subject is required", http.StatusBadRequest)
		return
	}

	if err := h.Store.Clear(subject); err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/displaynames/... -v`
Expected: PASS — all tests from Task 2 and Task 3 green (12 total).

- [ ] **Step 5: Wire into `internal/server` — add the field, routes, and `/api/me` display name**

In `backend/internal/server/server.go`:

Add the import:
```go
	"github.com/kenissha/DevPlatform/backend/internal/displaynames"
```

Add a field to `Deps` (after the existing `Access *access.Store` field, around line 50):
```go
	// DisplayNames overrides how a subject's name appears in the panel
	// (see internal/displaynames — the SSO JWT carries no name claim, only
	// subject/email). Optional: a nil Store means everyone falls back to
	// their email, matching today's behavior.
	DisplayNames *displaynames.Store
```

In `NewRouter`, after the existing `accessHandlers := &access.Handlers{Store: deps.Access}` line:
```go
	displayNameHandlers := &displaynames.Handlers{Store: deps.DisplayNames}
```

Change the `/api/me` line from:
```go
	mux.Handle("GET /api/me", authMiddleware(handleMe(deps.Users)))
```
to:
```go
	mux.Handle("GET /api/me", authMiddleware(handleMe(deps.Users, deps.DisplayNames)))
```

After the three existing `/api/access` route registrations, add:
```go
	mux.Handle("GET /api/display-names", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.List))))
	mux.Handle("PUT /api/display-names/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.Set))))
	mux.Handle("DELETE /api/display-names/{subject}", authMiddleware(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(displayNameHandlers.Clear))))
```

Change `handleMe`'s signature and body from:
```go
func handleMe(registry *users.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Just-in-time provisioning. A registry write failure must not fail
		// the request: the caller is authenticated either way, and losing
		// their entry costs an assignee-picker row, not access.
		if registry != nil {
			if _, err := registry.Upsert(user.Subject, user.Email, string(user.Role)); err != nil {
				log.Printf("handleMe: failed to record user %q: %v", user.Subject, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(user); err != nil {
			log.Printf("handleMe: failed to encode response: %v", err)
		}
	})
}
```
to:
```go
// meResponse is /api/me's response shape: the authenticated identity plus
// a display name resolved via internal/displaynames — kept as a local
// wrapper rather than a field on auth.User itself, since auth.User is used
// internally for authorization decisions that have nothing to do with how
// a name is displayed.
type meResponse struct {
	Subject     string    `json:"subject"`
	Email       string    `json:"email"`
	Role        auth.Role `json:"role"`
	DisplayName string    `json:"displayName"`
}

func handleMe(registry *users.Store, displayNames *displaynames.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Just-in-time provisioning. A registry write failure must not fail
		// the request: the caller is authenticated either way, and losing
		// their entry costs an assignee-picker row, not access.
		if registry != nil {
			if _, err := registry.Upsert(user.Subject, user.Email, string(user.Role)); err != nil {
				log.Printf("handleMe: failed to record user %q: %v", user.Subject, err)
			}
		}

		resp := meResponse{
			Subject:     user.Subject,
			Email:       user.Email,
			Role:        user.Role,
			DisplayName: displayNames.Get(user.Subject, user.Email),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("handleMe: failed to encode response: %v", err)
		}
	})
}
```

- [ ] **Step 6: Write a failing server-level test for the new field and routes**

`newTestRouter(t)` (already defined in `server_test.go`) wires a fixed set
of handlers and is called by ~25 existing tests as `newTestRouter(t)` —
changing its signature would touch every one of them for no reason this
task needs. Instead, build a standalone router directly in the new test,
the same way `newTestRouter` does internally, but with `DisplayNames` set.
Add the imports `"strings"` and
`"github.com/kenissha/DevPlatform/backend/internal/displaynames"` to
`server_test.go`'s existing import block, then add this test near the
existing `/api/me` tests (around line 157-170):

```go
func TestMe_IncludesDisplayNameOverride(t *testing.T) {
	dataDir := t.TempDir()
	displayNames := displaynames.NewStore(filepath.Join(dataDir, "display-names.json"))
	if err := displayNames.Set("dev-1", "Rifat Öztürk"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	router := NewRouter(Deps{
		GitHandler:     http.NotFoundHandler(),
		AuthMiddleware: testAuthMiddleware(),
		Users:          users.NewStore(filepath.Join(dataDir, "users.json")),
		DisplayNames:   displayNames,
	})

	rec := do(t, router, http.MethodGet, "/api/me", "dev-1", "developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Rifat") {
		t.Errorf("body = %q, want it to contain the display name override", rec.Body.String())
	}
}
```

Every other `Deps` field is left as its zero value (nil), which is safe
here: this test only ever routes to `/api/me`, and `handleMe` already
nil-checks `registry` before using it.

- [ ] **Step 7: Run the test to verify it fails, then implement, then verify it passes**

Run: `cd backend && go test ./internal/server/... -run TestMe_IncludesDisplayNameOverride -v`
Expected first: FAIL (`Deps` has no `DisplayNames` field yet, or
`handleMe` doesn't read it). After Step 5's edits are in place, re-run —
expected: PASS.

- [ ] **Step 8: Wire `DisplayNames` into `main.go`**

In `backend/cmd/devplatform/main.go`, add the import:
```go
	"github.com/kenissha/DevPlatform/backend/internal/displaynames"
```

Near the existing `accessStore := access.NewStore(...)` line, add:
```go
	displayNamesStore := displaynames.NewStore(filepath.Join(cfg.DataDir, "display-names.json"))
```

In the `server.Deps{...}` construction, add:
```go
		DisplayNames: displayNamesStore,
```

- [ ] **Step 9: Run the full backend test suite**

Run: `cd backend && go test ./...`
Expected: PASS, no regressions in `internal/server` or elsewhere.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/displaynames backend/internal/server backend/cmd/devplatform/main.go
git commit -m "feat(displaynames): expose admin API and surface displayName on /api/me"
```

---

### Task 4: Frontend — types, API client, and `User.displayName`

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

**Interfaces:**
- Consumes: `GET /api/display-names`, `PUT /api/display-names/{subject}`, `DELETE /api/display-names/{subject}`, `/api/me`'s new `displayName` field (Task 3).
- Produces: `User.displayName?: string`, `DisplayNameRegistry = Record<string, string>` type; `api.listDisplayNames()`, `api.setDisplayName(subject, name)`, `api.clearDisplayName(subject)`.

- [ ] **Step 1: Add the type**

In `frontend/src/api/types.ts`, find the `User` interface:
```ts
export interface User {
  subject: string
  email: string
  role: Role
}
```
Change it to:
```ts
export interface User {
  subject: string
  email: string
  role: Role
  // Set via backend/internal/displaynames — falls back to email when no
  // admin has configured an override for this subject (SSO's JWT carries
  // no name claim to use instead).
  displayName: string
}
```

Near `AccessRegistry`'s definition, add:
```ts
// Maps subject -> the display name an admin has set for them (see
// backend/internal/displaynames). A subject absent from this map has no
// override — the panel falls back to their email, same as User.displayName
// already does for /api/me's own caller.
export type DisplayNameRegistry = Record<string, string>
```

- [ ] **Step 2: Add the API client methods**

In `frontend/src/api/client.ts`, add `DisplayNameRegistry` to the type-only import list at the top (alongside `AccessRegistry`).

Near the existing `listAccess`/`setAccess`/`clearAccess` methods, add:
```ts
  // Per-person display-name override (Admin-only on the backend). A
  // subject absent from listDisplayNames's result falls back to their
  // email — see DisplayNameRegistry and User.displayName.
  listDisplayNames: () => request<DisplayNameRegistry>('/api/display-names'),
  setDisplayName: (subject: string, name: string) =>
    request<{ name: string }>(`/api/display-names/${encodeURIComponent(subject)}`, {
      method: 'PUT',
      body: JSON.stringify({ name }),
    }),
  clearDisplayName: (subject: string) =>
    request<void>(`/api/display-names/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npm run build`
Expected: FAILS — nothing consumes `listDisplayNames`/`setDisplayName`/`clearDisplayName` yet, which is fine (unused exports aren't a TypeScript error), but check the output for any real type error from this task's edits specifically (a mismatched field name, a missing import) before moving on.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat(frontend): add display-name types and API client methods"
```

---

### Task 5: Frontend — "Görünen adlar" section on `AccessPage`

**Files:**
- Modify: `frontend/src/pages/AccessPage.tsx`

**Interfaces:**
- Consumes: `api.listDisplayNames`, `api.setDisplayName`, `api.clearDisplayName` (Task 4); `api.listPeople()` (already used on this page).
- Produces: Nothing new consumed elsewhere — this is a leaf UI addition.

- [ ] **Step 1: Add state and loading for the display-name registry**

In `frontend/src/pages/AccessPage.tsx`, add `DisplayNameRegistry` to the
type-only import from `'../api/client'`.

Add a new state variable alongside the existing `registry` state:
```tsx
  const [displayNames, setDisplayNames] = useState<DisplayNameRegistry | null>(null)
```

Change `reload`'s `Promise.all` to also fetch it:
```tsx
  function reload() {
    Promise.all([api.listPeople(), api.listAccess(), api.listDisplayNames()])
      .then(([p, r, d]) => {
        setPeople(p)
        setRegistry(r)
        setDisplayNames(d)
        setError(null)
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : 'Erişim bilgisi yüklenemedi'))
  }
```

- [ ] **Step 2: Add a new section below the existing repo-access list**

After the closing `</div>` of the existing `<div className="card">...</div>`
block (the repo-access list), add a new section:

```tsx
      <div className="section-title">
        <h2>Görünen adlar</h2>
        <p className="page-subtitle">Girişte kullanılan e-posta yerine panelde gösterilecek ad-soyad</p>
      </div>
      <div className="card">
        {people === null && <p className="empty-state">Yükleniyor...</p>}
        {people && people.length > 0 && displayNames && (
          <ul className="row-list">
            {people.map((person) => (
              <DisplayNameRow
                key={person.subject}
                subject={person.subject}
                fallback={person.email || person.subject}
                current={displayNames[person.subject]}
                onChange={reload}
              />
            ))}
          </ul>
        )}
      </div>
```

- [ ] **Step 3: Add the `DisplayNameRow` component**

At the bottom of the file, after the existing `AccessEditor` function, add:

```tsx
function DisplayNameRow({
  subject,
  fallback,
  current,
  onChange,
}: {
  subject: string
  fallback: string
  current: string | undefined
  onChange: () => void
}) {
  const [name, setName] = useState(current ?? '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  async function save() {
    setSaving(true)
    setSaveError(null)
    try {
      if (name.trim()) {
        await api.setDisplayName(subject, name.trim())
      } else {
        await api.clearDisplayName(subject)
      }
      onChange()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : 'Kaydedilemedi')
    } finally {
      setSaving(false)
    }
  }

  return (
    <li>
      <div className="row-main">
        <span className="row-title">{fallback}</span>
        <span className="spacer" />
        <input
          type="text"
          value={name}
          placeholder={fallback}
          disabled={saving}
          onChange={(e) => setName(e.target.value)}
          className="inline-input"
        />
        <button type="button" className="btn-ghost" disabled={saving} onClick={save}>
          Kaydet
        </button>
        {saveError && <span className="error">{saveError}</span>}
      </div>
    </li>
  )
}
```

- [ ] **Step 4: Add CSS for the new input**

`.inline-select` (already in `index.css`) is styled for a subtle,
sentence-embedded look — not right for a field an admin is meant to
notice and type into. Add a sibling rule instead. In
`frontend/src/index.css`, near the existing `.inline-select` rules, add:

```css
.inline-input {
  width: 200px;
  font-size: 13px;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--surface-raised);
  color: var(--text);
}

.inline-input:focus {
  border-color: var(--accent-border);
  outline: none;
}
```

- [ ] **Step 5: Type-check and manually verify**

Run: `cd frontend && npm run build`
Expected: builds clean.

Run `npm run dev`, log in as an admin, open "Proje erişimi", confirm the
new "Görünen adlar" section lists every known person with an editable
field, and that saving one updates it (reload the page to confirm it
persisted).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/AccessPage.tsx frontend/src/index.css
git commit -m "feat(frontend): add display-name editor to Proje Erişimi page"
```

---

### Task 6: Frontend — `AppLayout` shows the display name

**Files:**
- Modify: `frontend/src/components/AppLayout.tsx`

**Interfaces:**
- Consumes: `User.displayName` (Task 4, already populated by `/api/me` as of Task 3).

- [ ] **Step 1: Use `displayName` for the shown name and initials**

In `frontend/src/components/AppLayout.tsx`, find:
```tsx
  const initials = (user?.email || user?.subject || '?').slice(0, 2)
```
Change to:
```tsx
  const shownName = user?.displayName || user?.email || user?.subject || '?'
  const initials = shownName.slice(0, 2)
```

Find:
```tsx
              <span className="muted">{user.email || user.subject}</span>
```
Change to:
```tsx
              <span className="muted">{shownName}</span>
```

- [ ] **Step 2: Type-check and manually verify**

Run: `cd frontend && npm run build`
Expected: builds clean.

With a display name configured for the logged-in test user (via Task 5's
editor), confirm the top bar shows the name instead of the email, and the
avatar initials come from the name.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/AppLayout.tsx
git commit -m "feat(frontend): show display name in the top bar"
```

---

### Task 7: Frontend — `RepoTabBar` component + sidebar simplification

**Files:**
- Create: `frontend/src/components/RepoTabBar.tsx`
- Modify: `frontend/src/components/AppLayout.tsx`
- Modify: `frontend/src/index.css`

**Interfaces:**
- Consumes: nothing new — reuses `OverviewIcon`, `TaskIcon`, `MergeIcon`, `BranchIcon`, `ChartIcon`, `DeployIcon` from `components/icons.tsx`, and `NavLink` from `react-router-dom`.
- Produces: `RepoTabBar({ repo: string })` component, rendered by `AppLayout` above `<Outlet />` whenever a repo route is active.

- [ ] **Step 1: Create `RepoTabBar`**

Create `frontend/src/components/RepoTabBar.tsx`:

```tsx
import { NavLink } from 'react-router-dom'
import { BranchIcon, ChartIcon, DeployIcon, MergeIcon, OverviewIcon, TaskIcon } from './icons'

function tabClass({ isActive }: { isActive: boolean }) {
  return isActive ? 'repo-tab active' : 'repo-tab'
}

// RepoTabBar is the GitHub-style horizontal nav shown above a repo's
// sub-pages once AppLayout has determined which repo the current route is
// showing (see AppLayout's useMatch-based `repo` detection). It replaces
// the old scheme of mixing this nav into the global sidebar: those two nav
// levels (cross-repo vs. inside-one-repo) were previously in the same
// list, which is what made "where am I" hard to tell at a glance.
export function RepoTabBar({ repo }: { repo: string }) {
  const encoded = encodeURIComponent(repo)
  return (
    <nav className="repo-tabbar">
      <div className="repo-tabbar-title">{repo}</div>
      <ul className="repo-tab-list">
        <li>
          <NavLink end to={`/repos/${encoded}`} className={tabClass}>
            <OverviewIcon />
            <span>Genel bakış</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/tasks`} className={tabClass}>
            <TaskIcon />
            <span>Görevler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/merge-requests`} className={tabClass}>
            <MergeIcon />
            <span>Merge istekleri</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/branches`} className={tabClass}>
            <BranchIcon />
            <span>Branch'ler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/insights`} className={tabClass}>
            <ChartIcon />
            <span>İstatistikler</span>
          </NavLink>
        </li>
        <li>
          <NavLink to={`/repos/${encoded}/deployments`} className={tabClass}>
            <DeployIcon />
            <span>Deploy</span>
          </NavLink>
        </li>
      </ul>
    </nav>
  )
}
```

- [ ] **Step 2: Remove the repo-scoped block from the sidebar and render `RepoTabBar` instead**

In `frontend/src/components/AppLayout.tsx`, add the import:
```tsx
import { RepoTabBar } from './RepoTabBar'
```

Remove this entire block from the sidebar (it currently sits between the
global `nav-list` and the "Repolar" `sidebar-group`):
```tsx
          {repo && (
            <div className="sidebar-group">
              <div className="sidebar-heading">{repo}</div>
              <ul className="nav-list">
                <li>
                  <NavLink end to={`/repos/${encodeURIComponent(repo)}`} className={navClass}>
                    <OverviewIcon />
                    <span className="nav-label">Genel bakış</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/tasks`} className={navClass}>
                    <TaskIcon />
                    <span className="nav-label">Görevler</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/merge-requests`} className={navClass}>
                    <MergeIcon />
                    <span className="nav-label">Merge istekleri</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/branches`} className={navClass}>
                    <BranchIcon />
                    <span className="nav-label">Branch'ler</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/insights`} className={navClass}>
                    <ChartIcon />
                    <span className="nav-label">İstatistikler</span>
                  </NavLink>
                </li>
                <li>
                  <NavLink to={`/repos/${encodeURIComponent(repo)}/deployments`} className={navClass}>
                    <DeployIcon />
                    <span className="nav-label">Deploy</span>
                  </NavLink>
                </li>
              </ul>
            </div>
          )}
```

Remove the now-unused icon imports (`BranchIcon`, `ChartIcon`, `DeployIcon`
if `DeployIcon` isn't used elsewhere in this file — check first, it is
also used for the global "Deploy hedefleri" nav item, so keep it; remove
only `MergeIcon`, `TaskIcon`, `OverviewIcon` if they become unused —
`OverviewIcon` is also still used by the global "Panel" nav item, so keep
it too; only `MergeIcon`, `TaskIcon`, `BranchIcon`, `ChartIcon` become
fully unused by this file after the removal) from this file's import list.

Change the `main` element from:
```tsx
        <main className="main">
          <Outlet />
        </main>
```
to:
```tsx
        <main className="main">
          {repo && <RepoTabBar repo={repo} />}
          <Outlet />
        </main>
```

- [ ] **Step 3: Add CSS for the tab bar**

In `frontend/src/index.css`, add (near the existing `.sidebar`/`.nav-item`
rules, reusing the same tokens they use):

```css
.repo-tabbar {
  display: flex;
  align-items: center;
  gap: 4px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 20px;
  padding-bottom: 0;
  overflow-x: auto;
}

.repo-tabbar-title {
  font-weight: 600;
  color: var(--text-strong);
  margin-right: 16px;
  white-space: nowrap;
}

.repo-tab-list {
  display: flex;
  list-style: none;
  margin: 0;
  padding: 0;
  gap: 4px;
}

.repo-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  text-decoration: none;
  white-space: nowrap;
  font-size: 0.9rem;
}

.repo-tab:hover {
  color: var(--text);
}

.repo-tab.active {
  color: var(--text-strong);
  border-bottom-color: var(--accent);
}
```

- [ ] **Step 4: Type-check and manually verify**

Run: `cd frontend && npm run build`
Expected: builds clean — no unused-import errors (TypeScript's `noUnusedLocals`,
if enabled in `tsconfig.json`, will fail the build on a leftover unused
icon import; if it fails here, remove whichever import Step 2 missed).

Run `npm run dev`, open a repo, confirm the sidebar only shows global
items + the repo list, and a horizontal tab bar with the repo's sub-pages
appears above the page content.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/RepoTabBar.tsx frontend/src/components/AppLayout.tsx frontend/src/index.css
git commit -m "feat(frontend): move repo navigation into a top tab bar"
```

---

### Task 8: Frontend — `RepoTasksPage` becomes a kanban board

**Files:**
- Modify: `frontend/src/pages/RepoTasksPage.tsx`
- Modify: `frontend/src/index.css`

**Interfaces:**
- Consumes: `api.listTasks`, `api.updateTask`, `api.createTask`, `api.listPeople` (unchanged, already used by this page); `TASK_STATUSES`, `TASK_STATUS_LABELS`, `TASK_STATUS_BADGE` from `labels.ts`.
- Produces: Nothing new consumed elsewhere — this task's board still calls the same `updateTask` endpoint Task 9 will keep using.

This task replaces the flat `row-list` rendering with a 3-column board and
wires drag-and-drop status changes. The create-task form at the bottom is
unchanged. Per-card detail (assignee, urgent toggle, description) moves out
of the row and into Task 9's click-to-open panel — this task's cards are
deliberately minimal (title, urgent badge, assignee name).

- [ ] **Step 1: Replace the page body**

Replace the entire contents of `frontend/src/pages/RepoTasksPage.tsx` with:

```tsx
import { useEffect, useState, type DragEvent, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiError } from '../api/client'
import type { Person, Task, TaskStatus } from '../api/types'
import { TASK_STATUS_BADGE, TASK_STATUS_LABELS, TASK_STATUSES } from '../labels'

export function RepoTasksPage() {
  const { repo = '' } = useParams<{ repo: string }>()
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [error, setError] = useState<string | null>(null)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [assignee, setAssignee] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  function reload() {
    api
      .listTasks(repo)
      .then(setTasks)
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(reload, [repo])

  // The assignee picker lists people the platform has actually seen, so a
  // task can't be assigned to a misspelled name that then never shows up
  // on anyone's dashboard.
  useEffect(() => {
    api.listPeople().then(setPeople).catch(() => setPeople([]))
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createTask(repo, title.trim(), description.trim(), assignee.trim())
      setTitle('')
      setDescription('')
      setAssignee('')
      reload()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : 'Görev oluşturulamadı')
    } finally {
      setCreating(false)
    }
  }

  // Optimistic: the board should react the instant a card is dropped
  // rather than waiting a round-trip. If the API call fails, reload()
  // pulls the task's real (unchanged) column back from the server.
  async function setStatus(task: Task, status: TaskStatus) {
    if (task.status === status) return
    setTasks((prev) => (prev ? prev.map((t) => (t.id === task.id ? { ...t, status } : t)) : prev))
    try {
      await api.updateTask(repo, task.id, { status })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
      reload()
    }
  }

  function handleDrop(e: DragEvent<HTMLDivElement>, status: TaskStatus) {
    e.preventDefault()
    setDragOverStatus(null)
    const taskId = e.dataTransfer.getData('text/plain')
    const task = tasks?.find((t) => t.id === taskId)
    if (task) setStatus(task, status)
  }

  function personLabel(subject: string): string {
    const person = people.find((p) => p.subject === subject)
    return person?.email || subject
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="page-title-group">
          <h1>Görevler</h1>
          <p className="page-subtitle">{repo} üzerindeki iş takibi</p>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {tasks === null && <p className="empty-state">Yükleniyor...</p>}
      {tasks && (
        <div className="kanban-board">
          {TASK_STATUSES.map((status) => {
            const columnTasks = tasks.filter((t) => t.status === status)
            return (
              <div
                key={status}
                className={dragOverStatus === status ? 'kanban-column drag-over' : 'kanban-column'}
                onDragOver={(e) => {
                  e.preventDefault()
                  setDragOverStatus(status)
                }}
                onDragLeave={() => setDragOverStatus((s) => (s === status ? null : s))}
                onDrop={(e) => handleDrop(e, status)}
              >
                <div className="kanban-column-header">
                  <span className={`badge ${TASK_STATUS_BADGE[status]}`}>{TASK_STATUS_LABELS[status]}</span>
                  <span className="muted">{columnTasks.length}</span>
                </div>
                <div className="kanban-column-body">
                  {columnTasks.map((task) => (
                    <div
                      key={task.id}
                      className="kanban-card"
                      draggable
                      onDragStart={(e) => e.dataTransfer.setData('text/plain', task.id)}
                    >
                      {task.urgent && <span className="badge badge-danger">Acil</span>}
                      <p className="kanban-card-title">{task.title}</p>
                      <p className="kanban-card-meta">
                        {task.assignedTo ? personLabel(task.assignedTo) : 'Atanmamış'}
                      </p>
                    </div>
                  ))}
                  {columnTasks.length === 0 && <p className="empty-state">Görev yok.</p>}
                </div>
              </div>
            )
          })}
        </div>
      )}

      <div className="section-title">
        <h2>Yeni görev</h2>
      </div>
      <div className="card">
        <div className="card-body">
          <form onSubmit={handleCreate}>
            <div className="field">
              <label htmlFor="task-title">Başlık</label>
              <input id="task-title" value={title} onChange={(e) => setTitle(e.target.value)} required />
            </div>
            <div className="field">
              <label htmlFor="task-description">Açıklama</label>
              <textarea
                id="task-description"
                rows={3}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
            <div className="field">
              <label htmlFor="task-assignee">Atanan</label>
              <select id="task-assignee" value={assignee} onChange={(e) => setAssignee(e.target.value)}>
                <option value="">Atanmamış</option>
                {people.map((p) => (
                  <option key={p.subject} value={p.subject}>
                    {p.subject}
                    {p.email ? ` (${p.email})` : ''}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-actions">
              <button type="submit" className="btn-primary" disabled={creating || !title.trim()}>
                {creating ? 'Oluşturuluyor...' : 'Görev oluştur'}
              </button>
              {createError && <p className="error">{createError}</p>}
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Add kanban CSS**

In `frontend/src/index.css`, add:

```css
.kanban-board {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}

.kanban-column {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
  min-height: 120px;
}

.kanban-column.drag-over {
  border-color: var(--accent-border);
  background: var(--accent-soft);
}

.kanban-column-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.kanban-column-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.kanban-card {
  background: var(--surface-raised);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px;
  cursor: grab;
}

.kanban-card:active {
  cursor: grabbing;
}

.kanban-card-title {
  margin: 6px 0 4px;
  font-weight: 500;
  color: var(--text-strong);
}

.kanban-card-meta {
  margin: 0;
  font-size: 0.85rem;
  color: var(--text-muted);
}
```

- [ ] **Step 3: Type-check and manually verify**

Run: `cd frontend && npm run build`
Expected: builds clean.

Run `npm run dev`, open a repo's Görevler tab, confirm: three columns
render with the right counts, dragging a card to another column persists
(reload the page — the task's column should stick), and the create-task
form at the bottom still works.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/RepoTasksPage.tsx frontend/src/index.css
git commit -m "feat(frontend): turn the task list into a kanban board"
```

---

### Task 9: Frontend — task detail panel on card click

**Files:**
- Modify: `frontend/src/pages/RepoTasksPage.tsx`
- Modify: `frontend/src/index.css`

**Interfaces:**
- Consumes: `Task`, `api.updateTask` (unchanged); the board rendered by Task 8.
- Produces: Nothing new consumed elsewhere.

Clicking a card (not dragging it) opens a detail panel with the controls
that used to live inline in the old list row: description, assignee
picker, urgent toggle, and a status picker (kept here too, since
drag-and-drop alone isn't keyboard-operable).

- [ ] **Step 1: Track the open task and distinguish click from drag**

In `frontend/src/pages/RepoTasksPage.tsx`, add a state variable for the
open task, and a ref to suppress a click that immediately follows a drag
(some browsers fire a click after a drag-and-drop sequence completes):

```tsx
import { useEffect, useRef, useState, type DragEvent, type FormEvent } from 'react'
```

```tsx
  const [openTask, setOpenTask] = useState<Task | null>(null)
  const draggingRef = useRef(false)
```

Update the card's `draggable` element to track drag state and open on
click:

```tsx
                    <div
                      key={task.id}
                      className="kanban-card"
                      draggable
                      onDragStart={(e) => {
                        draggingRef.current = true
                        e.dataTransfer.setData('text/plain', task.id)
                      }}
                      onDragEnd={() => {
                        draggingRef.current = false
                      }}
                      onClick={() => {
                        if (!draggingRef.current) setOpenTask(task)
                      }}
                    >
```

- [ ] **Step 2: Keep `openTask` in sync with `tasks` after a reload**

After a background `reload()` (e.g. once the detail panel makes a change),
`openTask` would otherwise hold a stale copy. Add an effect:

```tsx
  useEffect(() => {
    if (!openTask || !tasks) return
    const fresh = tasks.find((t) => t.id === openTask.id)
    setOpenTask(fresh ?? null)
  }, [tasks, openTask?.id])
```

Place this after the existing `useEffect(reload, [repo])` block.

- [ ] **Step 3: Add the detail panel**

At the end of the returned JSX, just before the final closing `</div>` of
the page (after the "Yeni görev" card), add:

```tsx
      {openTask && (
        <TaskDetailPanel
          task={openTask}
          people={people}
          repo={repo}
          onClose={() => setOpenTask(null)}
          onChanged={reload}
        />
      )}
```

- [ ] **Step 4: Add the `TaskDetailPanel` component**

At the bottom of the file, add:

```tsx
function TaskDetailPanel({
  task,
  people,
  repo,
  onClose,
  onChanged,
}: {
  task: Task
  people: Person[]
  repo: string
  onClose: () => void
  onChanged: () => void
}) {
  const [error, setError] = useState<string | null>(null)

  async function patch(changes: Partial<{ status: TaskStatus; urgent: boolean; assignedTo: string }>) {
    setError(null)
    try {
      await api.updateTask(repo, task.id, changes)
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Görev güncellenemedi')
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h3>{task.title}</h3>
          <button type="button" className="btn-ghost" onClick={onClose}>
            Kapat
          </button>
        </div>

        {error && <p className="error">{error}</p>}

        {task.description && <p className="task-desc">{task.description}</p>}

        <div className="field">
          <label htmlFor="detail-status">Durum</label>
          <select
            id="detail-status"
            value={task.status}
            onChange={(e) => patch({ status: e.target.value as TaskStatus })}
          >
            {TASK_STATUSES.map((s) => (
              <option key={s} value={s}>
                {TASK_STATUS_LABELS[s]}
              </option>
            ))}
          </select>
        </div>

        <div className="field">
          <label htmlFor="detail-assignee">Atanan</label>
          <select
            id="detail-assignee"
            value={task.assignedTo}
            onChange={(e) => patch({ assignedTo: e.target.value })}
          >
            <option value="">Atanmamış</option>
            {people.map((p) => (
              <option key={p.subject} value={p.subject}>
                {p.subject}
              </option>
            ))}
            {task.assignedTo && !people.some((p) => p.subject === task.assignedTo) && (
              <option value={task.assignedTo}>{task.assignedTo}</option>
            )}
          </select>
        </div>

        <button type="button" className="btn-secondary btn-sm" onClick={() => patch({ urgent: !task.urgent })}>
          {task.urgent ? 'Acili kaldır' : 'Acil işaretle'}
        </button>

        <p className="row-meta">
          {task.author} açtı · {formatDate(task.createdAt)}
        </p>
      </div>
    </div>
  )
}
```

Add `formatDate` to the existing `labels.ts` import at the top of the
file (it's already exported there, just not currently imported by this
page since Task 8 removed the old row that used it):
```tsx
import { TASK_STATUS_BADGE, TASK_STATUS_LABELS, TASK_STATUSES, formatDate } from '../labels'
```

- [ ] **Step 5: Add modal CSS**

In `frontend/src/index.css`, add:

```css
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}

.modal {
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 10px;
  padding: 20px;
  width: 420px;
  max-width: 90vw;
  max-height: 85vh;
  overflow-y: auto;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.modal-header h3 {
  margin: 0;
}
```

- [ ] **Step 6: Type-check and manually verify**

Run: `cd frontend && npm run build`
Expected: builds clean.

Run `npm run dev`, click a task card (a plain click, not a drag): confirm
the detail panel opens with status/assignee/urgent controls, changes save
and reflect on the board, and dragging a card still works without opening
the panel (i.e. `draggingRef` correctly suppresses the click).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/RepoTasksPage.tsx frontend/src/index.css
git commit -m "feat(frontend): add task detail panel on kanban card click"
```
