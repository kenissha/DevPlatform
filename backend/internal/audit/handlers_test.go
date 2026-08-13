package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kenissha/DevPlatform/backend/internal/access"
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

func newTestMux(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /api/audit", auth.RequireAuth([]byte(testJWTSecret), http.HandlerFunc(h.List)))
	return mux
}

// TestList_NarrowsToAllowedReposForARestrictedDeveloper proves List's Access
// filtering: without it, a developer restricted away from "other" would see
// its events through this cross-repo endpoint the same way an unfiltered
// /api/tasks or /api/merge-requests ListAll would leak them — see
// mergerequest.TestListAll_NarrowsToAllowedReposForARestrictedDeveloper for
// the equivalent test on that package's aggregate view.
func TestList_NarrowsToAllowedReposForARestrictedDeveloper(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err := logger.Log("dev-1", ActionTaskCreated, "allowed-repo", "t1", "izinli"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if err := logger.Log("dev-2", ActionTaskCreated, "other", "t2", "yasak"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	accessStore := access.NewStore(filepath.Join(t.TempDir(), "access.json"))
	if err := accessStore.Set("dev-1", []string{"allowed-repo"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	h := &Handlers{Logger: logger, Access: accessStore}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "dev-1", "developer"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var events []Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(events) != 1 || events[0].Repo != "allowed-repo" {
		t.Fatalf("events = %+v, want only the allowed-repo event", events)
	}
}

// TestList_AdminSeesEveryRepo proves an Admin's own restriction (if any)
// never narrows the audit log, matching access.RequireRepoAccess's own
// "RoleAdmin always satisfies any check" rule elsewhere in this codebase.
func TestList_AdminSeesEveryRepo(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err := logger.Log("dev-1", ActionTaskCreated, "allowed-repo", "t1", "izinli"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}
	if err := logger.Log("dev-2", ActionTaskCreated, "other", "t2", "diger"); err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	accessStore := access.NewStore(filepath.Join(t.TempDir(), "access.json"))
	if err := accessStore.Set("admin-1", []string{"allowed-repo"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	h := &Handlers{Logger: logger, Access: accessStore}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "admin-1", "admin"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var events []Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (admin must see every repo)", len(events))
	}
}
