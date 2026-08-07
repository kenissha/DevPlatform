package gitauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireBasicAuth_RejectsMissingCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

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

func TestRequireBasicAuth_RejectsWrongCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	req.SetBasicAuth("user", "wrong-password")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireBasicAuth_AllowsCorrectCredentials(t *testing.T) {
	handler := RequireBasicAuth("user", "pass", stubHandler())

	req := httptest.NewRequest(http.MethodGet, "/git/repo.git/info/refs", nil)
	req.SetBasicAuth("user", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
