// Package auth authenticates DevPlatform's web/API requests against a JWT
// issued by an external identity system, and provides the central
// role-authorization layer the design doc calls for ("Yetki kontrolleri kod
// içinde merkezi bir katmandan yapılır") — callers check access through
// RequireRole rather than scattering ad-hoc role comparisons.
//
// This is deliberately not a direct AD/LDAP integration: the real login
// happens in an existing external system the user already has (itself
// backed by AD/LDAP), and DevPlatform trusts a JWT that system hands it —
// a single-sign-on handoff rather than DevPlatform doing its own LDAP bind.
//
// CONFIGURE BEFORE PRODUCTION USE: the token verification below assumes
// HS256 (a shared secret, via Config.JWTSecret) and reads the role from a
// "role" claim and the user identifier from the standard "sub" claim. If
// the external system that issues these tokens uses a different signing
// algorithm (e.g. RS256 with a public key), a different claim name for the
// role, or an issuer/audience that should be validated, adjust
// parseAndValidate and the Claims struct below accordingly — this package
// is intentionally the one place that needs to change.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Role is one of the two fixed roles the design doc defines. There is no
// hierarchy encoded elsewhere in the codebase — RequireRole below is the
// only place that decides whether one role satisfies another.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
)

// User is the authenticated identity attached to a request's context after
// RequireAuth succeeds.
type User struct {
	Subject string `json:"subject"`
	Email   string `json:"email"`
	Role    Role   `json:"role"`
}

// claims is the expected shape of the external system's JWT payload.
type claims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

type contextKey int

const userContextKey contextKey = 0

// UserFromContext returns the authenticated User attached by RequireAuth,
// if any.
func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userContextKey).(*User)
	return u, ok
}

// withUser attaches user to ctx the same way RequireAuth does. Exported
// only within the package — tests for RequireRole use it to exercise that
// middleware directly, without needing a real signed token.
func withUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// RequireAuth returns middleware that validates a Bearer JWT in the
// Authorization header against secret, and attaches the resulting User to
// the request context for downstream handlers (see UserFromContext) and
// RequireRole. Requests with a missing, malformed, or invalid token receive
// 401 and never reach next.
func RequireAuth(secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := bearerToken(r)
		if err != nil {
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := parseAndValidate(token, secret)
		if err != nil {
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := withUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole wraps next with a check that the authenticated user (already
// attached to the request context by RequireAuth, which must run first)
// holds role. RoleAdmin always satisfies any RequireRole check, matching
// the design doc's "Yönetici — tam yetkili" description; there is no
// broader hierarchy than that.
func RequireRole(role Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		if user.Role != role && user.Role != RoleAdmin {
			http.Error(w, "403 Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", errors.New("missing or malformed Authorization header")
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

func parseAndValidate(tokenString string, secret []byte) (*User, error) {
	var c claims
	_, err := jwt.ParseWithClaims(tokenString, &c, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Method.Alg())
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}

	role := Role(strings.ToLower(c.Role))
	if role != RoleAdmin && role != RoleDeveloper {
		return nil, fmt.Errorf("unknown role claim %q", c.Role)
	}

	return &User{
		Subject: c.Subject,
		Email:   c.Email,
		Role:    role,
	}, nil
}
