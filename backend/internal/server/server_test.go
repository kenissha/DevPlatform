package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
)

const testSecret = "test-secret"

func testAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testSecret), next)
	}
}

func testMergeRequestHandlers(t *testing.T) *mergerequest.Handlers {
	t.Helper()
	dataDir := t.TempDir()
	return &mergerequest.Handlers{
		Store: mergerequest.NewStore(dataDir + "/merge-requests"),
		Repos: repostore.New(dataDir),
	}
}

func signTestToken(t *testing.T, subject, role string) string {
	t.Helper()
	c := jwt.MapClaims{
		"sub":   subject,
		"email": subject + "@example.com",
		"role":  role,
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

func TestHealthz_ReturnsOK(t *testing.T) {
	router := NewRouter(http.NotFoundHandler(), testAuthMiddleware(), testMergeRequestHandlers(t))
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

func TestMe_RejectsUnauthenticatedRequest(t *testing.T) {
	router := NewRouter(http.NotFoundHandler(), testAuthMiddleware(), testMergeRequestHandlers(t))
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMe_ReturnsAuthenticatedUser(t *testing.T) {
	router := NewRouter(http.NotFoundHandler(), testAuthMiddleware(), testMergeRequestHandlers(t))
	token := signTestToken(t, "user-1", "developer")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got auth.User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if got.Subject != "user-1" || got.Email != "user-1@example.com" || got.Role != auth.RoleDeveloper {
		t.Errorf("user = %+v, unexpected fields", got)
	}
}

func TestMergeRequestApprove_RejectsNonAdminThroughRouter(t *testing.T) {
	router := NewRouter(http.NotFoundHandler(), testAuthMiddleware(), testMergeRequestHandlers(t))
	token := signTestToken(t, "dev-1", "developer")
	req := httptest.NewRequest(http.MethodPost, "/api/repos/sample/merge-requests/0123456789abcdef/approve", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestMergeRequestList_ReturnsNotFoundForUnknownRepo(t *testing.T) {
	router := NewRouter(http.NotFoundHandler(), testAuthMiddleware(), testMergeRequestHandlers(t))
	token := signTestToken(t, "dev-1", "developer")
	req := httptest.NewRequest(http.MethodGet, "/api/repos/does-not-exist/merge-requests", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
