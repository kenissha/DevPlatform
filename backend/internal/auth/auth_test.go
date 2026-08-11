package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

func stubHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func signToken(t *testing.T, secret string, c claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return s
}

func validClaims() claims {
	return claims{
		Email: "dev@example.com",
		Role:  "developer",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
}

func TestRequireAuth_RejectsMissingHeader(t *testing.T) {
	handler := RequireAuth([]byte(testSecret), stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_RejectsMalformedHeader(t *testing.T) {
	handler := RequireAuth([]byte(testSecret), stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "not-a-bearer-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_RejectsWrongSignature(t *testing.T) {
	handler := RequireAuth([]byte(testSecret), stubHandler())
	token := signToken(t, "wrong-secret", validClaims())
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_RejectsExpiredToken(t *testing.T) {
	handler := RequireAuth([]byte(testSecret), stubHandler())
	c := validClaims()
	c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	token := signToken(t, testSecret, c)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_RejectsUnknownRole(t *testing.T) {
	handler := RequireAuth([]byte(testSecret), stubHandler())
	c := validClaims()
	c.Role = "superuser"
	token := signToken(t, testSecret, c)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_AllowsValidToken(t *testing.T) {
	var gotUser *User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth([]byte(testSecret), next)
	token := signToken(t, testSecret, validClaims())
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUser == nil {
		t.Fatal("expected User to be attached to request context")
	}
	if gotUser.Subject != "user-1" || gotUser.Email != "dev@example.com" || gotUser.Role != RoleDeveloper {
		t.Errorf("user = %+v, unexpected fields", gotUser)
	}
}

func TestRequireRole_RejectsWrongRole(t *testing.T) {
	handler := RequireRole(RoleAdmin, stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/admin-only", nil)
	ctx := req.Context()
	req = req.WithContext(withUser(ctx, &User{Subject: "u", Role: RoleDeveloper}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireRole_AllowsMatchingRole(t *testing.T) {
	handler := RequireRole(RoleDeveloper, stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/dev-only", nil)
	ctx := req.Context()
	req = req.WithContext(withUser(ctx, &User{Subject: "u", Role: RoleDeveloper}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRole_AdminSatisfiesAnyRole(t *testing.T) {
	handler := RequireRole(RoleDeveloper, stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/dev-only", nil)
	ctx := req.Context()
	req = req.WithContext(withUser(ctx, &User{Subject: "u", Role: RoleAdmin}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRole_RejectsUnauthenticatedRequest(t *testing.T) {
	handler := RequireRole(RoleDeveloper, stubHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/dev-only", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
