# Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In-panel + (placeholder) email notifications for the two triggers the design doc names as buildable today — task assignment and a merge request opened — closing the last open item in Faz 1 (see `docs/DURUM.md`).

**Architecture:** A new `internal/notify` package follows the exact file-per-record, per-recipient-subdirectory pattern `internal/taskboard` already established (see `taskboard.go`): one JSON file per notification under `<dataDir>/notifications/<recipient-subject>/`. Two existing handlers gain one line each to create a notification at the moment they already know who should hear about it — `taskboard.Handlers.Create`/`Update` (assignee) and `mergerequest.Handlers.Create` (every Admin, resolved from `internal/users.Store.List()`). Email is explicitly **not** wired to a real SMTP send in this plan — `internal/notify.EmailSender` is an interface with a logging-only default implementation, matching `docs/DURUM.md`'s own scoping ("gerçek gönderim arayüz arkasına alınır").

**Tech Stack:** Go 1.22+ (backend, matching every existing internal package), React + TypeScript (frontend, matching the existing pages under `frontend/src/pages/`). No new dependencies in either.

## Global Constraints

- Follow `internal/taskboard`'s file-store conventions exactly: `regexp`-validated identifiers before any path join, `os.O_CREATE|os.O_EXCL` for atomic create-with-generated-ID, `0o750`/`0o640` permissions, sentinel errors (`ErrInvalidRecipient`, `ErrInvalidID`, `ErrNotFound`) checked with `errors.Is` in the HTTP layer.
- Every new HTTP handler is mounted behind `auth.RequireAuth` in `server.go`, matching every existing route — no unauthenticated endpoint. A user may only read/mark-read their **own** notifications (the authenticated subject from `auth.UserFromContext`, not a path/query parameter) — there is no "list anyone's notifications" endpoint, admin or otherwise.
- Task assignment and merge-request-opened are the only two triggers in this plan. "Deploy sonucu" (deploy result), named in the design doc's original notification wishlist, is explicitly out of scope — Faz 2 (build/deploy automation) doesn't exist yet, so there is nothing to notify about.
- Email: add `SMTPHost`, `SMTPPort`, `SMTPFrom` to `config.Config` as placeholders (empty-string defaults, following the same doc-comment convention as `JWTSecret`'s "configure before production use" note) and a `notify.EmailSender` interface. Do **not** implement a real `net/smtp` send in this plan — the default implementation logs what it would have sent (recipient, subject) and returns nil. Wiring a real sender is future work once the config values have somewhere real to point.
- Commit after every task; each commit must leave `go build ./...`, `go vet ./...`, and `go test ./...` (backend) and `npm run build && npm run lint` (frontend, Task 3 only) passing.
- All code comments in English; commit messages Conventional-Commits-ish (`feat:`/`fix:`/`test:`), in English. UI copy (labels, page text) in Turkish, matching every existing page.

---

### Task 1: `internal/notify` package + HTTP API + email placeholder

**Files:**
- Create: `backend/internal/notify/notify.go`
- Create: `backend/internal/notify/handlers.go`
- Test: `backend/internal/notify/notify_test.go`
- Test: `backend/internal/notify/handlers_test.go`
- Modify: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- Modify: `backend/internal/server/server.go`
- Modify: `backend/cmd/devplatform/main.go`

**Interfaces:**
- Consumes: nothing from other new-this-plan code (standalone package, like `taskboard`).
- Produces:
  - `notify.Store` with `NewStore(rootDir string) *Store`, `(*Store) Create(recipientSubject, kind, message, link string) (Notification, error)`, `(*Store) ListForUser(subject string) ([]Notification, error)`, `(*Store) MarkRead(subject, id string) error`.
  - `notify.Notification{ID, Recipient, Kind, Message, Link string; Read bool; CreatedAt time.Time}` (JSON-tagged, mirroring `taskboard.Task`'s field style).
  - `notify.EmailSender` interface with one method, `Send(to, subject, body string) error`, and `notify.NoopEmailSender` (or similarly named) as the logging-only default.
  - `notify.Handlers{Store *notify.Store}` with `List` and `MarkRead` as `http.HandlerFunc`s — Task 2's trigger wiring calls `Store.Create` directly (not through `Handlers`), the same way `taskboard.Handlers.Create` calls `Store.Create` directly rather than through another HTTP round-trip.
  - `config.Config` gains `SMTPHost`, `SMTPPort`, `SMTPFrom string`.

- [ ] **Step 1: Write the failing store tests**

Create `backend/internal/notify/notify_test.go`, following `internal/taskboard/taskboard_test.go`'s structure and naming conventions (read that file first for the exact style — table-driven where the existing file uses tables, one test per behavior otherwise). Cover:
- `TestCreate_PersistsAndReturnsNotification` — create, confirm `ID`, `CreatedAt`, `Read: false` are populated, and `Recipient`/`Kind`/`Message`/`Link` match input.
- `TestCreate_RejectsInvalidRecipient` — mirror `taskboard`'s `validRepoName`-style rejection test, using whatever recipient-identifier validation you choose (subjects are opaque strings from the JWT `sub` claim — validate them the same defensive way `taskboard` validates repo names, e.g. `^[a-zA-Z0-9_.@-]+$`, since a subject is attacker-influenced input that ends up in a filesystem path exactly like a repo name does).
- `TestListForUser_ReturnsOnlyThatUsersNotifications` — create notifications for two different recipients, confirm `ListForUser` for one doesn't leak the other's.
- `TestListForUser_NewestFirst` — mirrors `taskboard.Store.List`'s sort-by-`CreatedAt`-descending test.
- `TestListForUser_ReturnsEmptySliceForUnknownUser` — mirrors `taskboard`'s missing-directory-is-empty-not-error test.
- `TestMarkRead_SetsReadTrue` — create, mark read, `ListForUser` (or a `Get`, your choice — check what's simplest given your actual `Store` shape) shows `Read: true`.
- `TestMarkRead_RejectsUnknownID` — returns `ErrNotFound`.
- `TestMarkRead_CannotMarkAnotherUsersNotificationRead` — critical: create a notification for recipient A, attempt `MarkRead("B", thatNotificationID)`, confirm it fails (`ErrNotFound`, not some other recipient's notification silently succeeding) — this is the store-level half of the "you can only touch your own notifications" constraint; Task 1 Step 5 covers the same property at the HTTP layer, but it must hold at the store layer too since `Handlers.MarkRead` is expected to just forward the authenticated subject into this call.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/notify/... -v` from `backend/`.
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `notify.go`**

Read `backend/internal/taskboard/taskboard.go` in full first — this file should closely mirror its structure (imports, sentinel errors, ID generation via `crypto/rand`+`hex`, atomic `O_CREATE|O_EXCL` create loop, `path` helper, `List`/`ListForUser` returning `[]T{}` not `nil` on a missing directory, `sort.Slice` newest-first). The one structural difference: `taskboard` groups files by *repo*; `notify` groups files by *recipient subject* — same shape, different grouping key. Implement:

```go
// Package notify persists per-user notifications and provides a minimal
// email-sending interface for future wiring. It groups records by
// recipient subject the same way internal/taskboard groups records by
// repository — one JSON file per notification under a per-recipient
// subdirectory.
package notify

// (imports, sentinel errors, recipient/id validation regexps — mirror
// taskboard.go's shape exactly)

type Notification struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Link      string    `json:"link"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct{ rootDir string }

func NewStore(rootDir string) *Store { return &Store{rootDir: rootDir} }

func (s *Store) Create(recipient, kind, message, link string) (Notification, error) { /* ... */ }
func (s *Store) ListForUser(recipient string) ([]Notification, error) { /* ... */ }
func (s *Store) MarkRead(recipient, id string) error { /* ... */ }
```

`MarkRead`'s implementation must resolve the file path from **both** `recipient` and `id` (i.e. `filepath.Join(rootDir, recipient, id+".json")`), never from `id` alone — that's what makes "can't mark another user's notification read" structurally true rather than an access-check you could forget to add, the same defense-in-depth reasoning `repostore`'s name validation used.

Then add `EmailSender`:
```go
// EmailSender abstracts sending an email so a real SMTP implementation can
// be swapped in later without touching call sites. NoopEmailSender is the
// default: it logs what it would have sent and does not actually send
// anything, matching this plan's explicit scope (see the plan's Global
// Constraints — no real SMTP send yet, config values have nowhere real to
// point until a future plan wires one).
type EmailSender interface {
	Send(to, subject, body string) error
}

type NoopEmailSender struct{}

func (NoopEmailSender) Send(to, subject, body string) error {
	log.Printf("notify: (no-op email sender) would send to=%q subject=%q", to, subject)
	return nil
}
```

- [ ] **Step 4: Run store tests to verify they pass**

Run: `go test ./internal/notify/... -v`
Expected: PASS, all cases from Step 1.

- [ ] **Step 5: Write the failing handler tests**

Create `backend/internal/notify/handlers_test.go`, mirroring `internal/taskboard/handlers_test.go`'s style (read it first). Cover:
- `TestList_ReturnsOnlyAuthenticatedUsersNotifications` — build a request with a context carrying one user (use the same test-helper pattern `taskboard`/`mergerequest`'s handler tests use to inject an `auth.User` into context — check how they do it, likely a small unexported helper or direct `auth`-package test hook), confirm the response contains only that user's notifications even when the store has others'.
- `TestMarkRead_Handler_RejectsMarkingAnotherUsersNotification` — HTTP-layer version of the store-level test above: authenticated as user A, POST to mark-read an ID that belongs to user B, confirm 404 (not 200) and that B's notification is still unread afterward.
- `TestMarkRead_Handler_ReturnsNotFoundForUnknownID`.
- `TestList_RequiresAuthentication` / rely on `server.go`'s routing test coverage instead if that's how existing handler tests split auth-boundary testing from handler-logic testing — check the existing pattern in `taskboard`'s or `mergerequest`'s handler tests and follow whichever convention is already established, don't invent a third one.

- [ ] **Step 6: Run to verify they fail, then implement `handlers.go`**

Mirror `taskboard/handlers.go`'s shape:
```go
package notify

type Handlers struct {
	Store *Store
}

// List handles GET /api/notifications — only the authenticated user's own.
func (h *Handlers) List(w http.ResponseWriter, r *http.Request) { /* ... */ }

// MarkRead handles POST /api/notifications/{id}/read.
func (h *Handlers) MarkRead(w http.ResponseWriter, r *http.Request) { /* ... */ }
```
Both pull the authenticated subject via `auth.UserFromContext(r.Context())` (401 if absent, matching every other handler in this codebase) and pass it as the `recipient`/first argument into the `Store` calls above — never trust a client-supplied recipient identifier.

Run: `go test ./internal/notify/... -v` — expected PASS, all cases from Step 5.

- [ ] **Step 7: Add SMTP placeholder config**

Modify `backend/internal/config/config.go` — add `SMTPHost`, `SMTPPort`, `SMTPFrom string` fields, read via `getEnv("DEVPLATFORM_SMTP_HOST", "")` etc. (empty default — unlike `GitUsername`'s `"dev"` default, there's no sensible non-empty local-dev default for an SMTP host, and `NoopEmailSender` doesn't need one). Add a doc-comment note in the same style as the existing `JWTSecret` note, explaining these are unused placeholders until a real `EmailSender` implementation is wired in a future plan.

Add the mirrored test in `config_test.go` (`TestLoad_ReadsSMTPSettingsFromEnv`, following `TestLoad_ReadsGitCredentialsFromEnv`'s exact shape).

Run: `go test ./internal/config/... -v` — expected PASS.

- [ ] **Step 8: Wire routes into server.go and main.go**

Read `backend/internal/server/server.go` in full first (it's not large) to see exactly how `taskboard.Handlers`/`mergerequest.Handlers` are constructed, stored on the `Dependencies`-or-similarly-named struct, and mounted with `authMiddleware(...)`. Add, following that exact pattern:
```go
mux.Handle("GET /api/notifications", authMiddleware(http.HandlerFunc(notifications.List)))
mux.Handle("POST /api/notifications/{id}/read", authMiddleware(http.HandlerFunc(notifications.MarkRead)))
```
Modify `backend/cmd/devplatform/main.go` to construct `notify.NewStore(...)` (pick a subdirectory under `cfg.DataDir`, e.g. `filepath.Join(cfg.DataDir, "notifications")`, matching how other stores are rooted there) and the `notify.Handlers`, threading it through to `server.NewRouter`/whatever the current dependency-injection shape is — match the existing pattern exactly, don't introduce a new wiring convention for just this one package.

- [ ] **Step 9: Full build and test**

Run: `go build ./...`, `go vet ./...`, `go test ./...` from `backend/`.
Expected: all clean, full suite green.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/notify backend/internal/config backend/internal/server/server.go backend/cmd/devplatform/main.go
git commit -m "feat: add notify package, API, and SMTP config placeholders"
```

---

### Task 2: Wire task-assignment and merge-request-opened triggers

**Files:**
- Modify: `backend/internal/taskboard/handlers.go`, `backend/internal/taskboard/handlers_test.go`
- Modify: `backend/internal/mergerequest/handlers.go`, `backend/internal/mergerequest/handlers_test.go`
- Modify: `backend/cmd/devplatform/main.go` (if `taskboard.Handlers`/`mergerequest.Handlers` need a new field to hold a `*notify.Store` — see below)

**Interfaces:**
- Consumes: `notify.Store.Create(recipient, kind, message, link string) (notify.Notification, error)` (Task 1). Consumes `users.Store.List() ([]users.User, error)` (already exists) to resolve Admin recipients.
- Produces: no new exported symbols — this task only adds notification side-effects to two existing handlers' existing behavior.

- [ ] **Step 1: Add a `Notify *notify.Store` field to `taskboard.Handlers`**

Read `backend/internal/taskboard/handlers.go`'s existing `Handlers` struct (it already has `Audit *audit.Logger` as an optional field — "Audit is optional; a nil Logger records nothing"). Add `Notify *notify.Store` following the exact same optionality convention: a nil `Notify` should mean "create no notifications," not panic — check how `Audit`'s nil-safety is implemented (likely a nil-receiver-safe method or a nil check at each call site) and match it exactly for `Notify`.

- [ ] **Step 2: Write the failing test for task-assignment notification**

Add to `handlers_test.go` (`internal/taskboard`): `TestCreate_NotifiesAssignee` — create a task with a non-empty `AssignedTo`, confirm (via a `notify.Store` pointed at a temp dir, read back with `ListForUser`) that the assignee received a notification whose `Message` mentions the task title and whose `Kind` identifies it as an assignment. Also add `TestCreate_DoesNotNotifyWhenUnassigned` (empty `AssignedTo` — no notification should be created) and `TestUpdate_NotifiesNewAssigneeOnReassignment` (a `PATCH` that changes `AssignedTo` notifies the *new* assignee — re-read `Store.Update`'s signature first to confirm reassignment is detectable from the existing `*string` diff, which it is, per Task 1's read of `taskboard.go`).

- [ ] **Step 3: Implement — call `Notify.Create` from `Create` and `Update`**

In `Handlers.Create`, after the existing `h.Audit.Log(...)` call, add: if `task.AssignedTo != ""` and `h.Notify != nil`, call `h.Notify.Create(task.AssignedTo, "task_assigned", "<Turkish message mentioning task.Title and the repo>", "<link to the task, e.g. /repos/{repo}/tasks/{id}>")`. Match the audit log's existing Turkish-prose style (see `"Görev açıldı: "+task.Title`) for the notification message text — this codebase's convention is Turkish user-facing strings, English code/comments, consistently.

In `Handlers.Update`, add the equivalent when `req.AssignedTo != nil && *req.AssignedTo != ""` — notify whoever the task is assigned to *after* the update (the new assignee, not the old one, since reassignment is the event worth notifying about).

Run: `go test ./internal/taskboard/... -v` — expected PASS, including the new tests.

- [ ] **Step 4: Add a `Notify *notify.Store` and `Users *users.Store` field to `mergerequest.Handlers`**

Read `backend/internal/mergerequest/handlers.go`'s existing `Handlers` struct first. Add both fields (nil-safe for `Notify`, matching Step 1's convention; `Users` is required to resolve who the Admins are, so decide — by reading how this package already handles its other required dependencies — whether a nil `Users` should panic loudly at construction/first use or just skip notification like a nil `Notify` would; prefer whichever this codebase already does elsewhere for a "required collaborator" versus "optional collaborator" distinction).

- [ ] **Step 5: Write the failing test for merge-request-opened notification**

Add to `handlers_test.go` (`internal/mergerequest`): `TestCreate_NotifiesAllAdmins` — seed a `users.Store` with two Admins and one Developer, create a merge request, confirm (via `notify.Store.ListForUser`) both Admins got a notification and the Developer did not.

- [ ] **Step 6: Implement — call `Notify.Create` for each Admin from `Create`**

In `Handlers.Create` (the merge-request-open handler), after it's persisted: if `h.Notify != nil && h.Users != nil`, call `h.Users.List()`, filter to `Role == "admin"` (match `internal/auth`'s `Role`/`RoleAdmin` constant rather than a bare string literal, if that type is importable here without creating a dependency cycle — check; if it would create one, a string comparison against `"admin"` with a comment explaining why is an acceptable fallback), and call `h.Notify.Create` once per Admin with a message identifying the repo, the merge request's title, and source→target branches, and a link to the merge request detail page.

Run: `go test ./internal/mergerequest/... -v` — expected PASS, including the new test.

- [ ] **Step 7: Wire the new struct fields in main.go**

Modify `backend/cmd/devplatform/main.go` — pass the `*notify.Store` constructed in Task 1 (and the existing `*users.Store`) into both `taskboard.Handlers{...}` and `mergerequest.Handlers{...}` construction, matching however `Audit` is already threaded through there.

- [ ] **Step 8: Full build and test**

Run: `go build ./...`, `go vet ./...`, `go test ./...` from `backend/`.
Expected: full suite green, including `taskboard`, `mergerequest`, and `notify`.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/taskboard backend/internal/mergerequest backend/cmd/devplatform/main.go
git commit -m "feat: notify assignee on task assignment, notify admins on merge request open"
```

---

### Task 3: Frontend — unread counter + notification list

**Files:**
- Create: `frontend/src/pages/NotificationsPage.tsx`
- Modify: `frontend/src/api/client.ts`, `frontend/src/api/types.ts`
- Modify: `frontend/src/components/AppLayout.tsx`
- Modify: `frontend/src/App.tsx` (new route)
- Modify: `frontend/src/labels.ts` (if new badge/kind labels are needed, following `AUDIT_ACTION_LABELS`'s pattern)

**Interfaces:**
- Consumes: `GET /api/notifications`, `POST /api/notifications/{id}/read` (Task 1).
- Produces: no backend-facing interface — this is the leaf of the feature.

- [ ] **Step 1: Add the `Notification` type and API client methods**

Read `frontend/src/api/types.ts` first for the existing type shapes (e.g. `AuditEvent`, `Task`) to match field naming/casing exactly (camelCase, matching the Go JSON tags from Task 1). Add:
```ts
export interface Notification {
  id: string
  recipient: string
  kind: string
  message: string
  link: string
  read: boolean
  createdAt: string
}
```
In `frontend/src/api/client.ts`, add to the `api` object (matching the existing method style exactly):
```ts
listNotifications: () => request<Notification[]>('/api/notifications'),
markNotificationRead: (id: string) =>
  request<void>(`/api/notifications/${encodeURIComponent(id)}/read`, { method: 'POST' }),
```
Add `Notification` to the re-exported type list at the bottom of `client.ts`.

- [ ] **Step 2: Build the notifications page**

Create `frontend/src/pages/NotificationsPage.tsx`, closely mirroring `frontend/src/pages/AuditPage.tsx`'s structure (page header, card, `row-list`, loading/empty states) — read `AuditPage.tsx` in full first (already quoted above during planning; use it as the literal template). Differences from `AuditPage`: each row is a `<Link>` to `notification.link` (when non-empty) and, if `!notification.read`, shows an unread visual marker (check `frontend/src/index.css` for an existing unread/dot/highlight convention before inventing a new CSS class — if `index.css` already has something like a `dot` or `unread` utility class from another feature, reuse it) and calls `api.markNotificationRead(id)` on click (optimistically flip local state to read, matching how other pages handle optimistic updates after a mutation — check `RepoMergeRequestsPage.tsx` or similar for the existing optimistic-update convention and match it).

- [ ] **Step 3: Add the route**

Modify `frontend/src/App.tsx` to add a route for the new page (e.g. `/notifications`), matching the existing route registration pattern for `/audit`.

- [ ] **Step 4: Add the unread counter to the top bar**

Modify `frontend/src/components/AppLayout.tsx`. Add a notifications entry to the sidebar `nav-list` (matching the existing `<NavLink>` items' exact shape — icon, label, `navClass`) that shows an unread count badge when > 0. This needs the unread count available in `AppLayout` — read `frontend/src/repos/ReposContext.tsx` first (it's the existing example of "fetch something once at the app shell level, expose it via context, poll or refetch as needed") and add an equivalent `NotificationsContext` (or extend an existing suitable context if one already fits better — check `AuthContext.tsx` too before deciding) that fetches `api.listNotifications()` on mount and re-fetches on a simple interval (e.g. every 30s via `setInterval` in a `useEffect`, cleared on unmount) — no WebSocket/SSE, that's out of scope (YAGNI: a 2-person team doesn't need real-time push for this).

- [ ] **Step 5: Build and lint**

Run from `frontend/`: `npm run build && npm run lint`.
Expected: both clean, matching the two pre-existing harmless `only-export-components` warnings at most (don't introduce new ones — if your new context file exports both a component and non-component values, split them the same way `AuthContext.tsx`/`ReposContext.tsx` apparently already tolerate, or restructure to avoid a new instance of the warning if that's straightforward).

- [ ] **Step 6: Manual smoke check (documented, not automated)**

This plan doesn't add frontend automated tests (none exist yet in this codebase for any page — matching the established convention rather than introducing one unilaterally). Instead, in your report, describe having started both processes per `docs/DURUM.md`'s "Nasıl çalıştırılır" section, generated a local JWT, logged in, assigned a task to yourself from a second (or the same) account, and confirmed a notification appeared and could be marked read. If you cannot actually run this (no display/browser available in your environment), say so explicitly rather than claiming it was checked.

- [ ] **Step 7: Commit**

```bash
git add frontend/src
git commit -m "feat: notifications page and unread counter in the top bar"
```

- [ ] **Step 8: Push**

```bash
git push origin main
```

---

## Self-Review Notes

- **Spec coverage:** Covers every item `docs/DURUM.md`'s "Sıradaki iş: bildirimler" section lists (`internal/notify`, both triggers, both endpoints, frontend counter + list, SMTP placeholder) except "deploy sonucu" as a trigger, which is explicitly named as out of scope in this plan's Global Constraints since Faz 2 doesn't exist yet — not an oversight.
- **Placeholder scan:** No TBD/TODO. Several steps say "read file X first and match its convention" rather than inlining every line of code verbatim — this is a deliberate choice for this plan (unlike the git-server plan, which inlined exact code because the APIs involved were novel/unverified): the conventions to match already exist in this same codebase, are internally consistent, and the risk of an implementer guessing wrong is much lower than researching an external alpha library's API surface. Each such step names the exact file to read and the exact property to match, not just "follow existing conventions" vaguely.
- **Type consistency:** `notify.Store`'s method signatures (Task 1) are used identically in Task 2's two call sites and Task 1's own `Handlers`. `Notification`'s JSON field names (Task 1, Go) match `Notification`'s TypeScript interface field names exactly (Task 3) — both camelCase, both listed side by side above.
- **Security:** every notification read/write path resolves the recipient from the authenticated JWT subject (`auth.UserFromContext`), never from a client-supplied value — Task 1's `MarkRead` signature takes `(subject, id)` specifically so a client cannot mark another user's notification read by guessing an ID, and this is tested at both the store and HTTP layers (Task 1, Steps 1 and 5). No new filesystem-path-building code skips the existing validate-before-join discipline established by `repostore`/`taskboard`/`mergerequest`.
