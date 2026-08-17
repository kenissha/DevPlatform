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
