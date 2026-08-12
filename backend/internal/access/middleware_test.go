package access

import (
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

func newTestMux(store *Store) *http.ServeMux {
	authMW := func(next http.Handler) http.Handler {
		return auth.RequireAuth([]byte(testJWTSecret), next)
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	mux.Handle("GET /api/repos/{repo}/thing", authMW(RequireRepoAccess(store, ok)))
	return mux
}

func doRequest(mux *http.ServeMux, repo, subject, role string, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/repos/"+repo+"/thing", nil)
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, subject, role))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRequireRepoAccess_AllowsAnUnrestrictedDeveloper(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	mux := newTestMux(store)

	rec := doRequest(mux, "intranet-backend", "dev-1", "developer", t)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequireRepoAccess_BlocksARestrictedDeveloperFromAnUngrantedRepo(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	if err := store.Set("dev-1", []string{"intranet-frontend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newTestMux(store)

	rec := doRequest(mux, "intranet-backend", "dev-1", "developer", t)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestRequireRepoAccess_AllowsARestrictedDeveloperTheirGrantedRepo(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	if err := store.Set("dev-1", []string{"intranet-backend"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newTestMux(store)

	rec := doRequest(mux, "intranet-backend", "dev-1", "developer", t)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequireRepoAccess_AlwaysAllowsAnAdminEvenWhenRestricted(t *testing.T) {
	store := NewStore(t.TempDir() + "/access.json")
	if err := store.Set("admin-1", []string{"some-other-repo"}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	mux := newTestMux(store)

	rec := doRequest(mux, "intranet-backend", "admin-1", "admin", t)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (admins always bypass)", rec.Code)
	}
}

func TestRequireRepoAccess_AllowsEveryRepoWithANilStore(t *testing.T) {
	mux := newTestMux(nil)

	rec := doRequest(mux, "any-repo", "dev-1", "developer", t)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
