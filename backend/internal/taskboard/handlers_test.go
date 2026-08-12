package taskboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
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

func addAuth(r *http.Request, t *testing.T, subject, role string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	return r
}

// newTestHandlers sets up a repostore with one repo ("sample") and a fresh
// taskboard.Store, wired together the same way server.NewRouter does.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	dataDir := t.TempDir()
	repos := repostore.New(dataDir)
	if _, err := repos.Create("sample"); err != nil {
		t.Fatalf("failed to create test repo: %v", err)
	}
	return &Handlers{
		Store: NewStore(dataDir + "/tasks"),
		Repos: repos,
	}
}

// newTestHandlersWithNotify is like newTestHandlers but also wires a
// notify.Store rooted at a fresh temp dir, so tests can assert on
// notifications via notify.Store.ListForUser.
func newTestHandlersWithNotify(t *testing.T) (*Handlers, *notify.Store) {
	t.Helper()
	h := newTestHandlers(t)
	n := notify.NewStore(t.TempDir())
	h.Notify = n
	return h, n
}

func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/repos/{repo}/tasks", authMW(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/repos/{repo}/tasks", authMW(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/repos/{repo}/tasks/{id}", authMW(http.HandlerFunc(h.Get)))
	mux.Handle("PATCH /api/repos/{repo}/tasks/{id}", authMW(http.HandlerFunc(h.Update)))
	return mux
}

func TestCreate_ReturnsCreatedTask(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":       "Fix login bug",
		"description": "Repro steps...",
		"assignedTo":  "dev-2",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/tasks", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var task Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if task.Author != "dev-1" || task.AssignedTo != "dev-2" || task.Status != StatusInProgress {
		t.Errorf("task = %+v, unexpected fields", task)
	}
}

func TestCreate_RejectsMissingTitle(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"description": "no title"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/tasks", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreate_RejectsUnknownRepo(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"title": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/does-not-exist/tasks", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestList_ReturnsCreatedTasks(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	if _, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/tasks", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var tasks []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
}

func TestGet_ReturnsTask(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/sample/tasks/"+created.ID, nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestUpdate_AppliesPartialChangeAsAnyAuthenticatedUser(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"status": "awaiting_test", "urgent": true})
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/sample/tasks/"+created.ID, bytes.NewReader(body))
	// A plain developer (not the assignee, not an admin) can still update
	// status/urgent — the board has no per-field authorization, see
	// Store.Update's doc comment.
	req = addAuth(req, t, "dev-2", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var task Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if task.Status != StatusAwaitingTest || !task.Urgent {
		t.Errorf("task = %+v, want status=awaiting_test urgent=true", task)
	}
	if task.AssignedTo != "dev-1" {
		t.Errorf("AssignedTo = %q, want unchanged %q", task.AssignedTo, "dev-1")
	}
}

func TestUpdateHandler_RejectsInvalidStatus(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"status": "bogus"})
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/sample/tasks/"+created.ID, bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreate_NotifiesAssignee(t *testing.T) {
	h, n := newTestHandlersWithNotify(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{
		"title":      "Fix login bug",
		"assignedTo": "dev-2",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/tasks", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	notifications, err := n.ListForUser("dev-2")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications for dev-2, want 1", len(notifications))
	}
	if notifications[0].Kind != "task_assigned" {
		t.Errorf("Kind = %q, want %q", notifications[0].Kind, "task_assigned")
	}
	if !strings.Contains(notifications[0].Message, "Fix login bug") {
		t.Errorf("Message = %q, want it to mention the task title", notifications[0].Message)
	}

	// The author must not also be notified — only the assignee.
	authorNotifications, err := n.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(authorNotifications) != 0 {
		t.Errorf("author got %d notifications, want 0", len(authorNotifications))
	}
}

func TestCreate_DoesNotNotifyWhenUnassigned(t *testing.T) {
	h, n := newTestHandlersWithNotify(t)
	mux := newMux(h)

	body, _ := json.Marshal(map[string]string{"title": "Unassigned task"})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/tasks", bytes.NewReader(body))
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	notifications, err := n.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(notifications) != 0 {
		t.Errorf("got %d notifications, want 0 for an unassigned task", len(notifications))
	}
}

func TestUpdate_NotifiesNewAssigneeOnReassignment(t *testing.T) {
	h, n := newTestHandlersWithNotify(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"assignedTo": "dev-3"})
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/sample/tasks/"+created.ID, bytes.NewReader(body))
	req = addAuth(req, t, "dev-2", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	newAssigneeNotifications, err := n.ListForUser("dev-3")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(newAssigneeNotifications) != 1 {
		t.Fatalf("got %d notifications for dev-3 (new assignee), want 1", len(newAssigneeNotifications))
	}
	if newAssigneeNotifications[0].Kind != "task_assigned" {
		t.Errorf("Kind = %q, want %q", newAssigneeNotifications[0].Kind, "task_assigned")
	}

	// The original assignee (dev-1) should not be notified on reassignment.
	oldAssigneeNotifications, err := n.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(oldAssigneeNotifications) != 0 {
		t.Errorf("old assignee got %d notifications, want 0", len(oldAssigneeNotifications))
	}
}

// TestUpdate_DoesNotNotifyOnSameAssigneeResend covers a PATCH that includes
// assignedTo but sets it to the value it already had — e.g. a client that
// echoes the whole task back when it only meant to change status or
// urgent. This must not re-notify the already-assigned person; only a
// genuine reassignment (a different AssignedTo value, see
// TestUpdate_NotifiesNewAssigneeOnReassignment) should.
func TestUpdate_DoesNotNotifyOnSameAssigneeResend(t *testing.T) {
	h, n := newTestHandlersWithNotify(t)
	mux := newMux(h)

	created, err := h.Store.Create("sample", "Fix login bug", "d", "dev-1", "dev-1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// The task is already assigned to dev-1; resend the same value
	// alongside an unrelated field change.
	body, _ := json.Marshal(map[string]any{"assignedTo": "dev-1", "urgent": true})
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/sample/tasks/"+created.ID, bytes.NewReader(body))
	req = addAuth(req, t, "dev-2", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	notifications, err := n.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(notifications) != 0 {
		t.Errorf("got %d notifications for dev-1 on a same-value resend, want 0", len(notifications))
	}
}
