package notify

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

func addAuth(r *http.Request, t *testing.T, subject, role string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	return r
}

// newTestHandlers sets up a fresh notify.Store, wired together the same
// way server.NewRouter does.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	return &Handlers{
		Store: NewStore(t.TempDir()),
	}
}

func newMux(h *Handlers) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/notifications", authMW(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/notifications/{id}/read", authMW(http.HandlerFunc(h.MarkRead)))
	return mux
}

func TestList_ReturnsOnlyAuthenticatedUsersNotifications(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	if _, err := h.Store.Create("dev-1", "task_assigned", "for dev-1", ""); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := h.Store.Create("dev-2", "task_assigned", "for dev-2", ""); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var notifications []Notification
	if err := json.Unmarshal(rec.Body.Bytes(), &notifications); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if notifications[0].Recipient != "dev-1" {
		t.Errorf("Recipient = %q, want %q", notifications[0].Recipient, "dev-1")
	}
}

func TestMarkRead_Handler_RejectsMarkingAnotherUsersNotification(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("dev-2", "task_assigned", "for dev-2", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/"+created.ID+"/read", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	notifications, err := h.Store.ListForUser("dev-2")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if notifications[0].Read {
		t.Error("expected dev-2's notification to remain unread")
	}
}

func TestMarkRead_Handler_ReturnsNotFoundForUnknownID(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/0123456789abcdef/read", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestMarkRead_Handler_MarksOwnNotificationRead(t *testing.T) {
	h := newTestHandlers(t)
	mux := newMux(h)

	created, err := h.Store.Create("dev-1", "task_assigned", "for dev-1", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/"+created.ID+"/read", nil)
	req = addAuth(req, t, "dev-1", "developer")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	notifications, err := h.Store.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser failed: %v", err)
	}
	if len(notifications) != 1 || !notifications[0].Read {
		t.Errorf("expected notification to be marked read, got %+v", notifications)
	}
}
