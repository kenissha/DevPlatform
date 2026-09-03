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
